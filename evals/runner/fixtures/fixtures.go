// Package fixtures generates input files for evals at run time. We
// deliberately don't commit binary fixtures to the repo: the runner
// produces what each case needs in its scratch workspace before the
// harness starts.
package fixtures

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-pdf/fpdf"
)

// Materialize produces the named fixture files in dstDir. Returns the
// list of resolved absolute paths in the same order as names. An
// unknown name is an error: every case's `files` field must reference
// a fixture that exists in this package.
func Materialize(dstDir string, names []string) ([]string, error) {
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w", dstDir, err)
	}
	out := make([]string, 0, len(names))
	for _, name := range names {
		path := filepath.Join(dstDir, name)
		if err := generate(name, path); err != nil {
			return nil, fmt.Errorf("fixture %s: %w", name, err)
		}
		out = append(out, path)
	}
	return out, nil
}

// generate dispatches by basename, accepting variants of the same
// fixture (invoice2.pdf, invoice_q3.pdf etc. all dispatch to the
// invoice generator). Variant support lets cases like AP-1 stage a
// folder of multiple PDFs of the same kind without bespoke generators
// for each filename.
func generate(name, path string) error {
	base := filepath.Base(name)
	switch {
	case base == "items.json":
		return writeJSON(path, sampleEvaluationItems())
	case base == "extractor.json":
		return writeJSON(path, sampleExtractorConfig())
	case base == "workflow-steps.json":
		return writeJSON(path, sampleWorkflowSteps())
	case strings.HasSuffix(base, ".pdf") && strings.HasPrefix(base, "invoice"):
		return writeInvoicePDF(path)
	case strings.HasSuffix(base, ".pdf") && strings.HasPrefix(base, "contract"):
		return writeContractPDF(path)
	case strings.HasSuffix(base, ".pdf") && strings.HasPrefix(base, "receipt"):
		return writeReceiptPDF(path)
	case strings.HasSuffix(base, ".pdf") && strings.HasPrefix(base, "combined"):
		return writeCombinedPDF(path)
	case base == "w2.pdf":
		return writeW2PDF(path)
	case base == "form1040.pdf":
		return writeForm1040PDF(path)
	default:
		return fmt.Errorf("no fixture generator for %q", name)
	}
}

// writeInvoicePDF emits a minimal one-page invoice PDF. The content
// matters very little — the agent's harness sees the file path and the
// stub returns canned extract responses; only the file's existence
// (and being a valid PDF) needs to be real.
func writeInvoicePDF(path string) error {
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.AddPage()
	pdf.SetFont("Helvetica", "B", 18)
	pdf.Cell(0, 12, "Acme Industries — Invoice INV-1024")
	pdf.Ln(15)
	pdf.SetFont("Helvetica", "", 11)
	pdf.Cell(0, 8, "Bill to: Wile E. Coyote")
	pdf.Ln(8)
	pdf.Cell(0, 8, "Date: 2026-04-15")
	pdf.Ln(12)
	pdf.SetFont("Helvetica", "B", 11)
	pdf.Cell(80, 8, "Description")
	pdf.Cell(30, 8, "Qty")
	pdf.Cell(30, 8, "Amount")
	pdf.Ln(8)
	pdf.SetFont("Helvetica", "", 11)
	for _, item := range []struct {
		desc, qty, amount string
	}{
		{"Widget", "2", "29.99"},
		{"Sprocket", "1", "14.50"},
		{"Cog (large)", "3", "9.50"},
	} {
		pdf.Cell(80, 8, item.desc)
		pdf.Cell(30, 8, item.qty)
		pdf.Cell(30, 8, item.amount)
		pdf.Ln(8)
	}
	pdf.Ln(8)
	pdf.SetFont("Helvetica", "B", 11)
	pdf.Cell(0, 8, "Total: 102.97")
	return pdf.OutputFileAndClose(path)
}

func writeContractPDF(path string) error {
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.AddPage()
	pdf.SetFont("Helvetica", "B", 18)
	pdf.Cell(0, 12, "Master Service Agreement")
	pdf.Ln(15)
	pdf.SetFont("Helvetica", "", 11)
	pdf.MultiCell(0, 7,
		"This Master Service Agreement ('Agreement') is entered into on April 15, 2026, "+
			"between Acme Industries ('Provider') and Wile E. Coyote ('Customer'). "+
			"Provider agrees to deliver services subject to the terms herein.",
		"", "", false)
	return pdf.OutputFileAndClose(path)
}

// writeCombinedPDF emits a multi-page PDF that simulates several
// customer statements glued together. The page count gives the agent
// something plausible to "split"; the stub returns canned splits
// regardless.
func writeCombinedPDF(path string) error {
	pdf := fpdf.New("P", "mm", "A4", "")
	for _, name := range []string{"Acme Industries", "Globex Corp", "Initech Inc"} {
		pdf.AddPage()
		pdf.SetFont("Helvetica", "B", 18)
		pdf.Cell(0, 12, "Customer Statement — "+name)
		pdf.Ln(15)
		pdf.SetFont("Helvetica", "", 11)
		pdf.MultiCell(0, 7,
			"Statement period: 2026-04-01 through 2026-04-30. "+
				"Outstanding balance: $1,234.56. Please remit by 2026-05-15.",
			"", "", false)
	}
	return pdf.OutputFileAndClose(path)
}

// writeW2PDF emits a fake W-2 PDF that carries source values the agent
// should *read* (not fill into). Pairs with form1040.pdf for the
// "fill a form from another document" recipe in skill.md. The contents
// are deliberately wage-statement-shaped so any model has something
// plausible to "extract" without a real schema.
func writeW2PDF(path string) error {
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.AddPage()
	pdf.SetFont("Helvetica", "B", 18)
	pdf.Cell(0, 12, "Form W-2 — Wage and Tax Statement (2025)")
	pdf.Ln(15)
	pdf.SetFont("Helvetica", "", 11)
	pdf.Cell(0, 8, "Employee: Wile E. Coyote")
	pdf.Ln(7)
	pdf.Cell(0, 8, "Employer: Acme Industries")
	pdf.Ln(7)
	pdf.Cell(0, 8, "Box 1 (Wages):           67,500.00")
	pdf.Ln(7)
	pdf.Cell(0, 8, "Box 2 (Federal tax):      9,800.00")
	pdf.Ln(7)
	pdf.Cell(0, 8, "Box 3 (Social Security): 67,500.00")
	pdf.Ln(7)
	pdf.Cell(0, 8, "Box 4 (Social tax):       4,185.00")
	return pdf.OutputFileAndClose(path)
}

// writeForm1040PDF emits a fake 1040 PDF: the *target* form whose
// fields the agent should fill. Field labels are written as text so a
// realistic agent flow (parse the form, decide which W-2 box maps
// where) has something to work against; the stub doesn't care about
// the bytes.
func writeForm1040PDF(path string) error {
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.AddPage()
	pdf.SetFont("Helvetica", "B", 18)
	pdf.Cell(0, 12, "Form 1040 — U.S. Individual Income Tax Return (Draft)")
	pdf.Ln(15)
	pdf.SetFont("Helvetica", "", 11)
	pdf.Cell(0, 8, "Name: ______________________________________")
	pdf.Ln(7)
	pdf.Cell(0, 8, "Line 1a (Wages from Form W-2 Box 1): __________")
	pdf.Ln(7)
	pdf.Cell(0, 8, "Line 25a (Federal tax withheld):     __________")
	pdf.Ln(7)
	pdf.Cell(0, 8, "Line 1z (Total wages):               __________")
	return pdf.OutputFileAndClose(path)
}

func writeReceiptPDF(path string) error {
	pdf := fpdf.New("P", "mm", "A6", "")
	pdf.AddPage()
	pdf.SetFont("Helvetica", "B", 14)
	pdf.Cell(0, 8, "Acme Cafe")
	pdf.Ln(10)
	pdf.SetFont("Helvetica", "", 9)
	pdf.Cell(0, 6, "2026-04-15  14:32")
	pdf.Ln(8)
	pdf.Cell(0, 6, "Coffee     $3.50")
	pdf.Ln(6)
	pdf.Cell(0, 6, "Bagel      $4.25")
	pdf.Ln(6)
	pdf.Cell(0, 6, "Tax        $0.62")
	pdf.Ln(6)
	pdf.SetFont("Helvetica", "B", 9)
	pdf.Cell(0, 6, "Total      $8.37")
	return pdf.OutputFileAndClose(path)
}

func writeJSON(path string, v any) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return jsonWrite(f, v)
}

func jsonWrite(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func sampleEvaluationItems() any {
	return []map[string]any{
		{
			"name": "Q3 invoice 1",
			"input": map[string]any{
				"fileId": "file_inv001",
			},
			"expectedOutput": map[string]any{
				"value": map[string]any{
					"invoice_id": "INV-1024",
					"total":      102.97,
				},
			},
		},
	}
}

func sampleExtractorConfig() any {
	return map[string]any{
		"name": "Q3 invoices",
		"config": map[string]any{
			"fields": []map[string]any{
				{"name": "invoice_id", "type": "string"},
				{"name": "total", "type": "number"},
				{"name": "line_items", "type": "array"},
			},
		},
	}
}

func sampleWorkflowSteps() any {
	return map[string]any{
		"steps": []map[string]any{
			{"name": "trigger", "type": "TRIGGER", "next": []map[string]any{{"step": "parse"}}},
			{"name": "parse", "type": "PARSE", "next": []map[string]any{{"step": "extract"}}},
			{
				"name": "extract",
				"type": "EXTRACT",
				"config": map[string]any{
					"extractor": map[string]any{"id": "ex_invoiceQ3", "version": "latest"},
				},
				"next": []map[string]any{{"step": "webhook"}},
			},
			{"name": "webhook", "type": "WEBHOOK_RESPONSE"},
		},
	}
}
