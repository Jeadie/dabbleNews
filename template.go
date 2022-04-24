package main

import (
	"bytes"
	"fmt"
	"github.com/Jeadie/godabble"
	"html/template"
	"math"
)

type EmailContent struct {
	Email    string
	Name     string
	News     []godabble.News
	Holdings []godabble.Holding
}

// ConstructEmail constructs a HTML email from an EmailContent.
func ConstructEmail(content EmailContent) string {
	content = FormatContent(content)

	tmpl := template.Must(template.ParseGlob("ui/*"))
	var b bytes.Buffer
	err := tmpl.ExecuteTemplate(&b, "Index", content)
	if err != nil {
		fmt.Printf("Could not generate template. Error: %s", err.Error())
		return ""
	}
	return b.String()
}

func FormatContent(content EmailContent) EmailContent {
	holdings := make([]godabble.Holding, len(content.Holdings))
	for i, h := range content.Holdings {
		h.Price = math.Round(h.Price*100)/100
		h.Movement1y = math.Round(h.Movement1y*100)/100
		h.Movement7d = math.Round(h.Movement7d*100)/100
		holdings[i] = h
	}
	content.Holdings = holdings
	return content
}