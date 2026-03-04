package output

import (
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"
)

// Table prints simple aligned tabular output.
type Table struct {
	headers []string
	rows    [][]string
}

func NewTable(headers ...string) *Table {
	return &Table{headers: headers}
}

func (t *Table) AddRow(cols ...string) {
	t.rows = append(t.rows, cols)
}

func (t *Table) Render(w io.Writer) error {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)

	if len(t.headers) > 0 {
		for i, h := range t.headers {
			if i > 0 {
				if _, err := fmt.Fprint(tw, "\t"); err != nil {
					return err
				}
			}
			if _, err := fmt.Fprint(tw, h); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprint(tw, "\n"); err != nil {
			return err
		}
	}

	for _, row := range t.rows {
		for i, col := range row {
			if i > 0 {
				if _, err := fmt.Fprint(tw, "\t"); err != nil {
					return err
				}
			}
			if _, err := fmt.Fprint(tw, col); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprint(tw, "\n"); err != nil {
			return err
		}
	}

	return tw.Flush()
}

func PrintJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
