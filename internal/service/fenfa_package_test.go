package service

import (
	"testing"

	"ordercore/internal/model"
)

func TestEffectivePackageFenFaRemark(t *testing.T) {
	o := &model.Order{
		FenFaRemark: "",
		Packages: []model.OrderPackage{
			{FenFaRemark: ""},
			{FenFaRemark: "1900"},
		},
	}
	if got := effectivePackageFenFaRemark(o); got != "1900" {
		t.Fatalf("got %q", got)
	}
	o.FenFaRemark = "header"
	if got := effectivePackageFenFaRemark(o); got != "header" {
		t.Fatalf("header prefer got %q", got)
	}
	o.FenFaRemark = ""
	o.Packages = []model.OrderPackage{
		{FenFaRemark: "1000"},
		{FenFaRemark: "900"},
	}
	if got := effectivePackageFenFaRemark(o); got != "1900" {
		t.Fatalf("sum got %q", got)
	}
}

func TestHydrateOrderPackageRemarks(t *testing.T) {
	o := &model.Order{
		Packages: []model.OrderPackage{{FenFaRemark: "1900", PrinterRemark: "p1"}},
	}
	hydrateOrderPackageRemarks(o)
	if o.FenFaRemark != "1900" || o.PrinterRemark != "p1" {
		t.Fatalf("hydrated fen=%q printer=%q", o.FenFaRemark, o.PrinterRemark)
	}
}
