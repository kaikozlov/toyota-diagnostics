package catalog

import (
	"context"
	"fmt"
	"strings"
)

func (d *DB) customizeArray(ctx context.Context, nodes []any, active referenceStack) ([]any, bool, error) {
	if len(nodes) < 32 {
		return nil, false, nil
	}
	ids := make([]int64, len(nodes))
	marker := ""
	for i, node := range nodes {
		ref, ok := node.(map[string]any)
		if !ok || len(ref) != 1 {
			return nil, false, nil
		}
		key := "$customize_item"
		v, ok := ref[key]
		if !ok {
			key = "$customize_group"
			v, ok = ref[key]
		}
		if !ok || marker != "" && key != marker {
			return nil, false, nil
		}
		marker = key
		id, e := referenceID(v, key)
		if e != nil {
			return nil, true, e
		}
		if active[referenceKey{key, id}] {
			return nil, true, fmt.Errorf("cyclic customize reference")
		}
		ids[i] = id
	}
	item := marker == "$customize_item"
	names := customizeGroupNames
	table := "customize_group_definitions"
	if item {
		names = customizeItemNames
		table = "customize_item_definitions"
	}
	fields := customizeFields(names, false)
	columns, joins := workColumns(fields, "d", "t")
	columns = append([]string{"d.id"}, columns...)
	if item {
		columns = append(columns, "d.choice_list_id", "l.entry_count", "d.executor_id", "e.id")
		joins = append(joins, "LEFT JOIN customize_choice_lists l ON l.id=d.choice_list_id", "LEFT JOIN customize_executor_profiles e ON e.id=d.executor_id")
		cols, j := workColumns(customizeFields(customizeExecutorNames, true), "e", "x")
		columns = append(columns, cols...)
		joins = append(joins, j...)
	}
	result := make([]any, len(ids))
	for start := 0; start < len(ids); start += 256 {
		end := start + 256
		if end > len(ids) {
			end = len(ids)
		}
		args := make([]any, end-start)
		for i := range args {
			args[i] = ids[start+i]
		}
		marks := strings.TrimSuffix(strings.Repeat("?,", len(args)), ",")
		rows, e := d.db.QueryContext(ctx, "SELECT "+strings.Join(columns, ",")+" FROM "+table+" d "+strings.Join(joins, " ")+" WHERE d.id IN("+marks+")", args...)
		if e != nil {
			return nil, true, e
		}
		found := map[int64]map[string]any{}
		lists := map[int64]int64{}
		counts := map[int64]int64{}
		for rows.Next() {
			values := make([]any, len(columns))
			dest := make([]any, len(values))
			for i := range dest {
				dest[i] = &values[i]
			}
			if e = rows.Scan(dest...); e != nil {
				rows.Close()
				return nil, true, e
			}
			id, ok := values[0].(int64)
			if !ok {
				rows.Close()
				return nil, true, fmt.Errorf("invalid customize ID")
			}
			out, err := workDecodedFields(fields, values[1:])
			if err != nil {
				rows.Close()
				return nil, true, err
			}
			if item {
				offset := 1 + len(fields)
				list, ok := values[offset].(int64)
				count, valid := values[offset+1].(int64)
				if !ok || !valid || count < 0 {
					rows.Close()
					return nil, true, fmt.Errorf("missing customize choice list")
				}
				lists[id] = list
				counts[list] = count
				if values[offset+2] != nil {
					if values[offset+3] == nil {
						rows.Close()
						return nil, true, fmt.Errorf("missing customize executor")
					}
					profile, err := workDecodedFields(customizeFields(customizeExecutorNames, true), values[offset+4:])
					if err != nil {
						rows.Close()
						return nil, true, err
					}
					out["modern_executor"] = profile
				}
			}
			found[id] = out
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return nil, true, e
		}
		if item {
			listArgs := make([]any, 0, len(counts))
			choices := map[int64][]any{}
			for id := range counts {
				listArgs = append(listArgs, id)
				choices[id] = []any{}
			}
			if len(listArgs) > 0 {
				cf := customizeFields(customizeChoiceNames, false)
				cc, cj := workColumns(cf, "c", "n")
				cc = append([]string{"m.list_id", "m.position", "c.id"}, cc...)
				query := "SELECT " + strings.Join(cc, ",") + " FROM customize_choice_members m LEFT JOIN customize_choice_definitions c ON c.id=m.choice_id " + strings.Join(cj, " ") + " WHERE m.list_id IN(" + strings.TrimSuffix(strings.Repeat("?,", len(listArgs)), ",") + ") ORDER BY m.list_id,m.position"
				cr, err := d.db.QueryContext(ctx, query, listArgs...)
				if err != nil {
					return nil, true, err
				}
				for cr.Next() {
					values := make([]any, len(cc))
					dest := make([]any, len(cc))
					for i := range dest {
						dest[i] = &values[i]
					}
					if err = cr.Scan(dest...); err != nil {
						cr.Close()
						return nil, true, err
					}
					list, ok := values[0].(int64)
					pos, valid := values[1].(int64)
					if !ok || !valid || pos != int64(len(choices[list])) || values[2] == nil {
						cr.Close()
						return nil, true, fmt.Errorf("invalid customize choice relationship")
					}
					choice, e := workDecodedFields(cf, values[3:])
					if e != nil {
						cr.Close()
						return nil, true, e
					}
					choices[list] = append(choices[list], choice)
				}
				err = cr.Err()
				cr.Close()
				if err != nil {
					return nil, true, err
				}
			}
			for list, count := range counts {
				if int64(len(choices[list])) != count {
					return nil, true, fmt.Errorf("incomplete customize choices")
				}
			}
			for id, out := range found {
				out["choices"] = choices[lists[id]]
			}
		}
		for i := start; i < end; i++ {
			out, ok := found[ids[i]]
			if !ok {
				return nil, true, fmt.Errorf("missing customize definition %d", ids[i])
			}
			result[i] = out
		}
	}
	return result, true, nil
}
