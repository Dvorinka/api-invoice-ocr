package ocr

type ExtractResult struct {
	SourceFilename    string     `json:"source_filename,omitempty"`
	SourceContentType string     `json:"source_content_type,omitempty"`
	TextLength        int        `json:"text_length"`
	UsedEngine        string     `json:"used_engine"`
	Warnings          []string   `json:"warnings,omitempty"`
	InvoiceNumber     string     `json:"invoice_number,omitempty"`
	SupplierName      string     `json:"supplier_name,omitempty"`
	VATNumber         string     `json:"vat_number,omitempty"`
	Currency          string     `json:"currency,omitempty"`
	TotalAmount       float64    `json:"total_amount,omitempty"`
	SubtotalAmount    float64    `json:"subtotal_amount,omitempty"`
	TaxAmount         float64    `json:"tax_amount,omitempty"`
	LineItems         []LineItem `json:"line_items,omitempty"`
	RawTextPreview    string     `json:"raw_text_preview"`
}

type LineItem struct {
	Description string  `json:"description"`
	Quantity    float64 `json:"quantity,omitempty"`
	UnitPrice   float64 `json:"unit_price,omitempty"`
	Total       float64 `json:"total,omitempty"`
}
