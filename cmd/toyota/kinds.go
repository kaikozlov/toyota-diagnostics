package main

import (
	"fmt"
	"github.com/kaikozlov/toyota-diagnostics/internal/catalog"
)

func (a *app) kinds(ecu *catalog.ECU) error {
	category := 0
	if ecu != nil {
		category = ecu.CategoryID
	}
	kinds, err := a.db.Kinds(a.ctx, a.opt.region, category)
	if err != nil {
		return err
	}
	if a.opt.json {
		return encode(kinds)
	}
	for _, kind := range kinds {
		fmt.Println(kind)
	}
	return nil
}

func (a *app) catalogKind(name string) (bool, error) {
	kinds, err := a.db.Kinds(a.ctx, a.opt.region, 0)
	if err != nil {
		return false, err
	}
	for _, kind := range kinds {
		if kind == name {
			return true, nil
		}
	}
	return false, nil
}
