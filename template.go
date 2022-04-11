package main

import (
	"bytes"
	"fmt"
	"github.com/Jeadie/godabble"
	"html/template"
)

type EmailContent struct {
	Email    string
	Name     string
	News     []godabble.News
	Holdings []godabble.Holding
}

// ConstructEmail constructs a HTML email from an EmailContent.
func ConstructEmail(content EmailContent) string {
	tmpl := template.Must(template.ParseGlob("ui/*"))
	var b bytes.Buffer
	err := tmpl.ExecuteTemplate(&b, "Index", content)
	if err != nil {
		fmt.Printf("Could not generate template. Error: %s", err.Error())
		return ""
	}
	return b.String()
}
