package catalog

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

func decodedDomain(name string) int {
	for i, k := range decodedMonitorNames {
		if k == name {
			return i
		}
	}
	return -1
}

type decodedLayout struct {
	columns, joins      []string
	ints, texts, ranges []string
}

func decodedColumns(spec decodedMonitorSpec) decodedLayout {
	l := decodedLayout{ints: strings.Fields(spec.integers), texts: strings.Fields(spec.texts), ranges: strings.Fields(spec.ranges)}
	l.columns = []string{"d.id"}
	for _, k := range l.ints {
		l.columns = append(l.columns, "d.\""+strings.ReplaceAll(k, ".", "__")+"\"")
	}
	for i, k := range l.texts {
		col := "d." + strings.ReplaceAll(k, ".", "__") + "_id"
		alias := fmt.Sprintf("t%d", i)
		l.columns = append(l.columns, col, alias+".value")
		l.joins = append(l.joins, "LEFT JOIN text_values "+alias+" ON "+alias+".id="+col)
	}
	for _, k := range l.ranges {
		l.columns = append(l.columns, "d."+k)
	}
	if spec.shared != "" {
		table, last := "freeze_units", "genre_id"
		if spec.shared == "pattern" {
			table, last = "freeze_patterns", "value"
		}
		l.joins = append(l.joins, "LEFT JOIN "+table+" v ON v.id=d.value_id", "LEFT JOIN text_values label ON label.id=v.text_id")
		l.columns = append(l.columns, "v.id", "v.record", "v.string_index", "v.table_type", "v.text_id", "label.value", "v."+last)
	}
	if spec.children {
		for _, child := range decodedChildren {
			l.columns = append(l.columns, "d."+child[0]+"_count")
		}
	}
	return l
}
func decodedScalar(spec decodedMonitorSpec, l decodedLayout, values []any) (map[string]any, []int64, error) {
	out := make(map[string]any, len(l.ints)+len(l.texts)+len(l.ranges)+4)
	pos := 0
	number := func() (int64, error) {
		v, ok := values[pos].(int64)
		pos++
		if !ok {
			return 0, fmt.Errorf("invalid decoded integer")
		}
		return v, nil
	}
	for _, k := range l.ints {
		v, e := number()
		if e != nil {
			return nil, nil, e
		}
		decodedPut(out, k, signalNumber(v))
	}
	for _, k := range l.texts {
		ident, label := values[pos], values[pos+1]
		pos += 2
		if ident != nil && label == nil {
			return nil, nil, fmt.Errorf("missing decoded text %s", k)
		}
		if label != nil {
			if _, ok := label.(string); !ok {
				return nil, nil, fmt.Errorf("invalid decoded text")
			}
		}
		decodedPut(out, k, label)
	}
	for _, k := range l.ranges {
		v, ok := values[pos].(float64)
		pos++
		if !ok {
			return nil, nil, fmt.Errorf("invalid decoded range")
		}
		decodedPut(out, k, signalNumber(v))
	}
	if spec.kind != "" {
		out["kind"] = spec.kind
	}
	if spec.shared != "" {
		shared, e := number()
		if e != nil || shared <= 0 {
			return nil, nil, fmt.Errorf("missing decoded physical value")
		}
		for _, k := range []string{"record", "name_string_index", "table"} {
			v, e := number()
			if e != nil {
				return nil, nil, e
			}
			out[k] = signalNumber(v)
		}
		textID, label := values[pos], values[pos+1]
		pos += 2
		if textID != nil && label == nil {
			return nil, nil, fmt.Errorf("missing decoded physical label")
		}
		out["name"] = label
		v, e := number()
		if e != nil {
			return nil, nil, e
		}
		field := "genre_id"
		if spec.shared == "pattern" {
			field = "value"
		}
		out[field] = signalNumber(v)
	}
	counts := []int64{}
	if spec.children {
		for _, child := range decodedChildren {
			n, e := number()
			if e != nil || n < 0 {
				return nil, nil, fmt.Errorf("invalid decoded relationship count")
			}
			counts = append(counts, n)
			out[child[0]] = []any{}
		}
	}
	return out, counts, nil
}

// Queries have two stable sizes (point or 256); NULL padding avoids an
// unbounded prepared-statement cache for the final partial chunk.
func (d *DB) decodedQuery(ctx context.Context, key, query string, args []any) (*sql.Rows, error) {
	d.decodedStatements.mu.Lock()
	if d.decodedStatements.statements == nil {
		d.decodedStatements.statements = map[string]*sql.Stmt{}
	}
	stmt := d.decodedStatements.statements[key]
	if stmt == nil {
		var err error
		stmt, err = d.db.PrepareContext(ctx, query)
		if err != nil {
			d.decodedStatements.mu.Unlock()
			return nil, err
		}
		d.decodedStatements.statements[key] = stmt
	}
	d.decodedStatements.mu.Unlock()
	return stmt.QueryContext(ctx, args...)
}
func (d *DB) decodedChunk(ctx context.Context, index int, ids []int64) (map[int64]any, error) {
	if len(ids) == 0 || len(ids) > 256 {
		return nil, fmt.Errorf("invalid decoded batch size")
	}
	spec := decodedMonitorSpecs[index]
	name := decodedMonitorNames[index]
	layout := decodedColumns(spec)
	size := 256
	if len(ids) == 1 {
		size = 1
	}
	args := make([]any, size)
	marks := make([]string, size)
	for i := range marks {
		marks[i] = "?"
	}
	for i, id := range ids {
		if id <= 0 {
			return nil, fmt.Errorf("invalid decoded reference")
		}
		args[i] = id
	}
	suffix := strings.Join(marks, ",")
	rows, err := d.decodedQuery(ctx, fmt.Sprintf("%s_scalar_%d", name, size), "SELECT "+strings.Join(layout.columns, ",")+" FROM decoded_"+name+" d "+strings.Join(layout.joins, " ")+" WHERE d.id IN("+suffix+")", args)
	if err != nil {
		return nil, err
	}
	found := map[int64]any{}
	counts := map[int64][]int64{}
	values := make([]any, len(layout.columns))
	scan := make([]any, len(values))
	for i := range scan {
		scan[i] = &values[i]
	}
	for rows.Next() {
		if err = rows.Scan(scan...); err != nil {
			rows.Close()
			return nil, err
		}
		id, ok := values[0].(int64)
		if !ok {
			rows.Close()
			return nil, fmt.Errorf("invalid decoded ID")
		}
		out, count, e := decodedScalar(spec, layout, values[1:])
		if e != nil {
			rows.Close()
			return nil, e
		}
		found[id] = out
		counts[id] = count
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		if found[id] == nil {
			return nil, fmt.Errorf("missing decoded %s definition %d", name, id)
		}
	}
	if spec.children {
		for relation, child := range decodedChildren {
			rows, err = d.decodedQuery(ctx, fmt.Sprintf("%s_%s_%d", name, child[0], size), "SELECT owner_id,position,child_id FROM decoded_"+name+"_"+child[0]+" WHERE owner_id IN("+suffix+") ORDER BY owner_id,position", args)
			if err != nil {
				return nil, err
			}
			for rows.Next() {
				var owner, pos, childID int64
				if err = rows.Scan(&owner, &pos, &childID); err != nil {
					rows.Close()
					return nil, err
				}
				out, ok := found[owner].(map[string]any)
				if !ok {
					rows.Close()
					return nil, fmt.Errorf("invalid decoded relationship owner")
				}
				list := out[child[0]].([]any)
				if pos != int64(len(list)) || childID <= 0 {
					rows.Close()
					return nil, fmt.Errorf("invalid decoded child relationship")
				}
				out[child[0]] = append(list, map[string]any{"$decoded_" + child[1]: signalNumber(childID)})
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return nil, err
			}
			for id, value := range found {
				if int64(len(value.(map[string]any)[child[0]].([]any))) != counts[id][relation] {
					return nil, fmt.Errorf("incomplete decoded child relationship")
				}
			}
		}
	}
	return found, nil
}

// Bulk restoration is bounded to 256 parent IDs. Child domains are leaf
// definitions; gather them once across the chunk instead of one SQL call for
// every occurrence. No expanded document is retained in a global cache.
func (d *DB) decodedExpandedChunk(ctx context.Context, index int, ids []int64) (map[int64]any, error) {
	found, err := d.decodedChunk(ctx, index, ids)
	if err != nil {
		return nil, err
	}
	if !decodedMonitorSpecs[index].children {
		return found, nil
	}
	for _, child := range decodedChildren {
		wanted := []int64{}
		seen := map[int64]bool{}
		for _, v := range found {
			for _, ref := range v.(map[string]any)[child[0]].([]any) {
				id, e := referenceID(ref.(map[string]any)["$decoded_"+child[1]], child[1])
				if e != nil {
					return nil, e
				}
				if !seen[id] {
					seen[id] = true
					wanted = append(wanted, id)
				}
			}
		}
		restored := map[int64]any{}
		for start := 0; start < len(wanted); start += 256 {
			end := start + 256
			if end > len(wanted) {
				end = len(wanted)
			}
			part, e := d.decodedChunk(ctx, decodedDomain(child[1]), wanted[start:end])
			if e != nil {
				return nil, e
			}
			for id, v := range part {
				restored[id] = v
			}
		}
		for _, v := range found {
			list := v.(map[string]any)[child[0]].([]any)
			for i, ref := range list {
				id, _ := referenceID(ref.(map[string]any)["$decoded_"+child[1]], child[1])
				list[i] = restored[id]
			}
		}
	}
	return found, nil
}

func (d *DB) decodedMonitorArray(ctx context.Context, nodes []any, active referenceStack) ([]any, bool, error) {
	if len(nodes) < 4 {
		return nil, false, nil
	}
	type request struct {
		index    int
		id       int64
		position int
	}
	requests := []request{}
	groups := map[int][]request{}
	for position, node := range nodes {
		obj, ok := node.(map[string]any)
		if !ok {
			continue
		}
		marker, raw, reference := storedReference(obj)
		if !reference || !strings.HasPrefix(marker, "$decoded_") {
			continue
		}
		index := decodedDomain(strings.TrimPrefix(marker, "$decoded_"))
		if index < 0 {
			continue
		}
		id, err := referenceID(raw, marker)
		if err != nil {
			return nil, true, err
		}
		if active[referenceKey{marker, id}] {
			return nil, true, fmt.Errorf("cyclic decoded reference")
		}
		r := request{index, id, position}
		requests = append(requests, r)
		groups[index] = append(groups[index], r)
	}
	if len(requests) == 0 {
		return nil, false, nil
	}
	out := append([]any(nil), nodes...)
	for index, group := range groups {
		for start := 0; start < len(group); start += 256 {
			end := start + 256
			if end > len(group) {
				end = len(group)
			}
			ids := make([]int64, end-start)
			for i := range ids {
				ids[i] = group[start+i].id
			}
			found, err := d.decodedExpandedChunk(ctx, index, ids)
			if err != nil {
				return nil, true, err
			}
			for _, r := range group[start:end] {
				out[r.position] = found[r.id]
			}
		}
	}
	return out, true, nil
}
