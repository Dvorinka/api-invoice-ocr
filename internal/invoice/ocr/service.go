package ocr

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"mime"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Service struct {
	maxFileSize int64
}

func NewService(maxFileSize int64) *Service {
	if maxFileSize <= 0 {
		maxFileSize = 10 << 20
	}
	return &Service{maxFileSize: maxFileSize}
}

func (s *Service) MaxFileSize() int64 {
	return s.maxFileSize
}

func (s *Service) ExtractFromFile(ctx context.Context, data []byte, filename, contentType string) (ExtractResult, error) {
	if len(data) == 0 {
		return ExtractResult{}, errors.New("file is empty")
	}
	if int64(len(data)) > s.maxFileSize {
		return ExtractResult{}, fmt.Errorf("file exceeds max size of %d bytes", s.maxFileSize)
	}

	text, engine, warnings, err := extractText(ctx, data, filename, contentType)
	if err != nil {
		return ExtractResult{}, err
	}

	result := parseInvoice(text)
	result.SourceFilename = filename
	result.SourceContentType = contentType
	result.UsedEngine = engine
	result.Warnings = warnings
	return result, nil
}

func (s *Service) ExtractFromText(text string) (ExtractResult, error) {
	if strings.TrimSpace(text) == "" {
		return ExtractResult{}, errors.New("text is required")
	}

	result := parseInvoice(text)
	result.UsedEngine = "provided_text"
	return result, nil
}

func extractText(ctx context.Context, data []byte, filename, contentType string) (string, string, []string, error) {
	ext := strings.ToLower(filepath.Ext(filename))
	mediaType, _, _ := mime.ParseMediaType(contentType)

	// Direct text formats are handled without OCR.
	if ext == ".txt" || strings.HasPrefix(mediaType, "text/") {
		return string(data), "plain_text", nil, nil
	}

	text, err := runTesseract(ctx, data, ext)
	if err != nil {
		return "", "", nil, err
	}
	return text, "tesseract", nil, nil
}

func runTesseract(ctx context.Context, data []byte, ext string) (string, error) {
	if ext == "" {
		ext = ".bin"
	}

	tmpInput, err := os.CreateTemp("", "invoice-upload-*"+ext)
	if err != nil {
		return "", err
	}
	tmpInputPath := tmpInput.Name()
	defer os.Remove(tmpInputPath)

	if _, err := tmpInput.Write(data); err != nil {
		_ = tmpInput.Close()
		return "", err
	}
	if err := tmpInput.Close(); err != nil {
		return "", err
	}

	ocrCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ocrCtx, "tesseract", tmpInputPath, "stdout")
	var output bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return "", errors.New("tesseract is not installed; use /v1/invoice/extract/text or install tesseract")
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("ocr failed: %s", msg)
	}

	text := output.String()
	if strings.TrimSpace(text) == "" {
		return "", errors.New("ocr produced no text")
	}
	return text, nil
}

var (
	invoiceNumberRe = regexp.MustCompile(`(?i)invoice\s*(?:no|number|#)?\s*[:\-]?\s*([A-Z0-9][A-Z0-9\-\/]{2,})`)
	vatRe           = regexp.MustCompile(`\b([A-Z]{2}[A-Z0-9]{6,14})\b`)
	amountTokenRe   = regexp.MustCompile(`(?i)(USD|EUR|GBP|CZK|\$|€|£)?\s*([0-9]{1,3}(?:[.,\s][0-9]{3})*(?:[.,][0-9]{2})?)`)
	lineItemRe      = regexp.MustCompile(`(?i)^(.+?)\s+([0-9]+(?:[.,][0-9]+)?)\s+x\s+([0-9]+(?:[.,][0-9]+)?)\s*=?\s*([0-9]+(?:[.,][0-9]+)?)$`)
)

func parseInvoice(text string) ExtractResult {
	normalized := strings.ReplaceAll(text, "\r\n", "\n")
	normalized = strings.TrimSpace(normalized)
	lines := splitNonEmptyLines(normalized)

	result := ExtractResult{
		TextLength:     len(normalized),
		RawTextPreview: preview(normalized, 500),
	}

	for _, line := range lines {
		if result.InvoiceNumber == "" {
			if match := invoiceNumberRe.FindStringSubmatch(line); len(match) > 1 {
				result.InvoiceNumber = strings.TrimSpace(match[1])
			}
		}

		if result.VATNumber == "" {
			upper := strings.ToUpper(line)
			lower := strings.ToLower(line)
			if strings.Contains(lower, "vat") || strings.Contains(lower, "tax id") || strings.Contains(lower, "tin") {
				if match := vatRe.FindStringSubmatch(upper); len(match) > 1 && containsDigit(match[1]) {
					result.VATNumber = strings.TrimSpace(match[1])
				}
			}
		}
	}

	result.SupplierName = detectSupplier(lines)
	result.Currency, result.TotalAmount = detectAmount(lines, []string{"total due", "amount due", "grand total", "total"})
	_, result.SubtotalAmount = detectAmount(lines, []string{"subtotal", "sub total"})
	_, result.TaxAmount = detectAmount(lines, []string{"tax", "vat"})
	result.LineItems = parseLineItems(lines)

	return result
}

func splitNonEmptyLines(text string) []string {
	rawLines := strings.Split(text, "\n")
	lines := make([]string, 0, len(rawLines))
	for _, line := range rawLines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			lines = append(lines, trimmed)
		}
	}
	return lines
}

func detectSupplier(lines []string) string {
	if len(lines) == 0 {
		return ""
	}

	stopWords := []string{"invoice", "bill to", "ship to", "date", "total", "subtotal"}
	for _, line := range lines[:min(6, len(lines))] {
		lower := strings.ToLower(line)
		skip := false
		for _, word := range stopWords {
			if strings.Contains(lower, word) {
				skip = true
				break
			}
		}
		if !skip && len(line) >= 3 {
			return line
		}
	}
	return ""
}

func detectAmount(lines []string, labels []string) (currency string, amount float64) {
	var bestAmount float64
	var bestCurrency string

	for _, line := range lines {
		lower := strings.ToLower(line)
		relevant := false
		for _, label := range labels {
			if strings.Contains(lower, label) {
				relevant = true
				break
			}
		}
		if !relevant {
			continue
		}

		curr, value, ok := parseAmount(line)
		if !ok {
			continue
		}
		if value > bestAmount {
			bestAmount = value
			bestCurrency = curr
		}
	}

	return bestCurrency, round2(bestAmount)
}

func parseLineItems(lines []string) []LineItem {
	items := make([]LineItem, 0, 10)
	for _, line := range lines {
		match := lineItemRe.FindStringSubmatch(line)
		if len(match) != 5 {
			continue
		}

		qty, _ := parseDecimal(match[2])
		unit, _ := parseDecimal(match[3])
		total, _ := parseDecimal(match[4])
		items = append(items, LineItem{
			Description: strings.TrimSpace(match[1]),
			Quantity:    round2(qty),
			UnitPrice:   round2(unit),
			Total:       round2(total),
		})
	}
	return items
}

func parseAmount(line string) (string, float64, bool) {
	matches := amountTokenRe.FindAllStringSubmatch(line, -1)
	if len(matches) == 0 {
		return "", 0, false
	}

	var bestAmount float64
	var bestCurrency string
	for _, match := range matches {
		if len(match) < 3 {
			continue
		}

		value, err := parseDecimal(match[2])
		if err != nil {
			continue
		}
		if value >= bestAmount {
			bestAmount = value
			bestCurrency = normalizeCurrency(match[1])
		}
	}
	if bestAmount == 0 {
		return "", 0, false
	}
	return bestCurrency, round2(bestAmount), true
}

func parseDecimal(raw string) (float64, error) {
	raw = strings.TrimSpace(raw)
	raw = strings.ReplaceAll(raw, " ", "")
	raw = strings.ReplaceAll(raw, ",", ".")

	parts := strings.Split(raw, ".")
	if len(parts) > 2 {
		// Handle thousand separators like 1.234.56.
		raw = strings.Join(parts[:len(parts)-1], "") + "." + parts[len(parts)-1]
	}
	return strconv.ParseFloat(raw, 64)
}

func normalizeCurrency(raw string) string {
	raw = strings.TrimSpace(strings.ToUpper(raw))
	switch raw {
	case "$":
		return "USD"
	case "€":
		return "EUR"
	case "£":
		return "GBP"
	default:
		return raw
	}
}

func preview(text string, max int) string {
	if len(text) <= max {
		return text
	}
	return text[:max]
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func containsDigit(value string) bool {
	for _, ch := range value {
		if ch >= '0' && ch <= '9' {
			return true
		}
	}
	return false
}
