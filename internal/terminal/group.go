package terminal

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/kaikozlov/toyota-diagnostics/internal/catalog"
)

// DID prints every Data List signal packed into one DID, in bit order.
func (p Printer) DID(e catalog.ECU, ident string, signals []catalog.ItemDetail) {
	p.title(p.accent(e.Key)+"  "+p.bold(ident), p.kind("did")+p.dim(" · "+e.Region))
	p.println(p.dim(e.Name + " · " + plural(len(signals), "signal", "signals")))
	p.println()
	type row struct {
		field, name, value, id string
		start                  int64
	}
	rows := make([]row, len(signals))
	for i, s := range signals {
		r := decode(s.Payload)
		rows[i] = row{field: "—", name: s.Name, id: "#" + strconv.FormatInt(s.ID, 10), start: 1 << 40}
		if start, ok := r.num("bit_start"); ok {
			rows[i].start = start
			if end, ok := r.num("bit_end"); ok {
				rows[i].field = fmt.Sprintf("%d", start)
				if end != start {
					rows[i].field = fmt.Sprintf("%d–%d", start, end)
				}
			}
		}
		var value []string
		if patterns, ok := r["patterns"].(map[string]any); ok && len(patterns) > 0 {
			value = append(value, plural(len(patterns), "state", "states"))
		}
		if u := r.str("unit"); u != "" {
			value = append(value, u)
		}
		if lo, hi, ok := r.pair("data_range"); ok && len(value) == 0 {
			value = append(value, fmt.Sprintf("%d … %d", lo, hi))
		}
		rows[i].value = strings.Join(value, " · ")
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].start < rows[j].start })
	fields, values, ids := make([]string, len(rows)), make([]string, len(rows)), make([]string, len(rows))
	for i, r := range rows {
		fields[i], values[i], ids[i] = r.field, r.value, r.id
	}
	fieldW, valueW, idW := max(4, colWidth(fields, 8)), colWidth(values, 18), colWidth(ids, 9)
	p.println(p.dim("  " + pad("bits", fieldW) + "  signal"))
	nameW := max(16, p.measure()-2-fieldW-2-valueW-2-idW-4)
	for _, r := range rows {
		line := "  " + pad(p.blue(r.field), fieldW) + "  " + pad(fit(r.name, nameW), nameW) + "  " +
			pad(p.dim(fit(r.value, valueW)), valueW) + "  " + p.dim(padLeft(r.id, idW))
		p.println(strings.TrimRight(line, " "))
	}
	p.footer("toyota inspect ID decodes one signal")
}
