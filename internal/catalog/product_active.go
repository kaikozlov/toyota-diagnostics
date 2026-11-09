package catalog

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

type operationWire struct {
	selector             string
	comm, timeout, retry int64
	send, mask, check    []byte
}

func (d *DB) operationWire(ctx context.Context, id int64) (operationWire, error) {
	var w operationWire
	var valid bool
	err := d.db.QueryRowContext(ctx, `SELECT t.value,w.comm_set,w.send,w.mask,w.check_bytes,w.receive_timeout,w.retry_count,
 typeof(w.send)='blob' AND typeof(w.mask)='blob' AND typeof(w.check_bytes)='blob'
 FROM operation_wire_frames w JOIN text_values t ON t.id=w.selector_id WHERE w.id=?`, id).Scan(&w.selector, &w.comm, &w.send, &w.mask, &w.check, &w.timeout, &w.retry, &valid)
	if err != nil {
		return w, fmt.Errorf("operation wire frame %d: %w", id, err)
	}
	if !valid {
		return w, fmt.Errorf("invalid operation wire bytes")
	}
	return w, nil
}
func (d *DB) operationDefinition(ctx context.Context, kind string, id int64) (any, error) {
	kinds := map[string]int64{"$operation_base_frame": 0, "$operation_frame": 1, "$operation_encoding": 2, "$operation_mask": 3, "$operation_conversion": 4}
	slot, ok := kinds[kind]
	if !ok || id <= 0 {
		return nil, fmt.Errorf("invalid operation reference")
	}
	cacheable := id <= (math.MaxInt64-4)/5
	key := int64(0)
	if cacheable {
		key = id*5 + slot
		if v, ok := d.operationDefinitions.load(key); ok {
			return v, nil
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var value map[string]any
	var err error
	switch kind {
	case "$operation_frame":
		value, err = d.operationFrame(ctx, id)
	case "$operation_base_frame":
		value, err = d.operationBaseFrame(ctx, id)
	default:
		value, err = d.operationPolicy(ctx, kind, id)
	}
	if err != nil {
		return nil, err
	}
	if cacheable {
		raw, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		d.operationDefinitions.store(key, value, len(raw))
	}
	return value, nil
}
func (d *DB) operationFrame(ctx context.Context, id int64) (map[string]any, error) {
	var wire int64
	var frame, name, session string
	err := d.db.QueryRowContext(ctx, `SELECT v.wire_id,f.value,n.value,s.value FROM operation_frames v
 JOIN text_values f ON f.id=v.frame_id JOIN text_values n ON n.id=v.name_id
 JOIN text_values s ON s.id=v.session_id WHERE v.id=?`, id).Scan(&wire, &frame, &name, &session)
	if err != nil {
		return nil, fmt.Errorf("operation frame %d: %w", id, err)
	}
	w, err := d.operationWire(ctx, wire)
	if err != nil {
		return nil, err
	}
	return map[string]any{"selector": w.selector, "comm_set": catalogNumber(w.comm), "send": hex.EncodeToString(w.send),
		"mask": hex.EncodeToString(w.mask), "check": hex.EncodeToString(w.check), "receive_timeout": catalogNumber(w.timeout),
		"retry_count": catalogNumber(w.retry), "frame": frame, "name": name, "session_requirement": session}, nil
}
func (d *DB) operationBaseFrame(ctx context.Context, id int64) (map[string]any, error) {
	var wire, category, param, unknown, exception, flag int64
	var frame string
	var labels [6]string
	err := d.db.QueryRowContext(ctx, `SELECT b.wire_id,b.category_id,f.value,s.value,sn.value,m.value,mn.value,c.value,cn.value,
 b.send_parameter,b.unknown_word,b.exception_id,b.exception_flag FROM operation_base_frames b
 JOIN text_values f ON f.id=b.frame_id JOIN text_values s ON s.id=b.send_id
 JOIN text_values sn ON sn.id=b.send_normalized_id JOIN text_values m ON m.id=b.mask_id
 JOIN text_values mn ON mn.id=b.mask_normalized_id JOIN text_values c ON c.id=b.check_id
 JOIN text_values cn ON cn.id=b.check_normalized_id WHERE b.id=?`, id).Scan(&wire, &category, &frame, &labels[0], &labels[1], &labels[2], &labels[3], &labels[4], &labels[5], &param, &unknown, &exception, &flag)
	if err != nil {
		return nil, fmt.Errorf("operation base frame %d: %w", id, err)
	}
	w, err := d.operationWire(ctx, wire)
	if err != nil {
		return nil, err
	}
	variable := func(i int, data []byte) any {
		return map[string]any{"id": labels[i*2], "normalized_id": labels[i*2+1], "bytes": hex.EncodeToString(data)}
	}
	meta := map[string]any{"comm_set_id": catalogNumber(w.comm), "receive_timeout": catalogNumber(w.timeout), "retry_count": catalogNumber(w.retry),
		"send_parameter": catalogNumber(param), "unknown_word_0c": catalogNumber(unknown), "exception_handler_id": catalogNumber(exception), "exception_handler_flag": catalogNumber(flag)}
	return map[string]any{"category_id": catalogNumber(category), "comm_frame_id": frame, "comm_set": catalogNumber(w.comm), "comm_set_metadata": meta,
		"kind": "frame", "send": variable(0, w.send), "receive_mask": variable(1, w.mask), "receive_check": variable(2, w.check), "selector": w.selector}, nil
}
func (d *DB) operationPolicy(ctx context.Context, kind string, id int64) (map[string]any, error) {
	table := ""
	var fields, columns []string
	switch kind {
	case "$operation_encoding":
		table = "operation_encoding_policies"
		fields = []string{"unsupported_status", "source_scope", "value_input", "adapter", "engine_selection"}
		columns = []string{"unsupported_status_id", "source_scope_id", "value_input_id", "adapter_id", "engine_selection_id"}
	case "$operation_mask":
		table = "operation_mask_policies"
		fields = []string{"start", "stop", "type67_rule"}
		columns = []string{"start_id", "stop_id", "rule_id"}
	case "$operation_conversion":
		table = "operation_conversions"
		fields = []string{"formula", "source", "value_integer"}
		columns = []string{"formula_id", "source_id", "value_integer_id"}
	default:
		return nil, fmt.Errorf("invalid operation policy")
	}
	selected, joins := []string{}, []string{}
	text := make([]string, len(fields))
	dest := make([]any, 0, len(fields)+2)
	for i, column := range columns {
		alias := fmt.Sprintf("t%d", i)
		selected = append(selected, alias+".value")
		joins = append(joins, "JOIN text_values "+alias+" ON "+alias+".id=p."+column)
		dest = append(dest, &text[i])
	}
	var p5, p6 int64
	if kind == "$operation_encoding" {
		selected = append(selected, "p.p5_supported", "p.p6_supported")
		dest = append(dest, &p5, &p6)
	}
	query := "SELECT " + strings.Join(selected, ",") + " FROM " + table + " p " + strings.Join(joins, " ") + " WHERE p.id=?"
	if err := d.db.QueryRowContext(ctx, query, id).Scan(dest...); err != nil {
		return nil, fmt.Errorf("operation policy %s/%d: %w", kind, id, err)
	}
	out := make(map[string]any, len(fields)+2)
	for i, k := range fields {
		out[k] = text[i]
	}
	if kind == "$operation_encoding" {
		if p5 < 0 || p5 > 1 || p6 < 0 || p6 > 1 {
			return nil, fmt.Errorf("invalid operation support flags")
		}
		out["p5_supported"] = p5 == 1
		out["p6_supported"] = p6 == 1
	}
	return out, nil
}
