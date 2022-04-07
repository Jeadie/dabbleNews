package main

import (
	"fmt"
	"github.com/mailjet/mailjet-apiv3-go/v3"
	"os"
)

const defaultSubject = "News for you"
const defaultTextPart = "Here's what you need to know"

type Emailer struct {
	client *mailjet.Client
	from   *mailjet.RecipientV31
}

func ConstructEmailer() *Emailer {
	return &Emailer{
		client: mailjet.NewMailjetClient(
			os.Getenv("MJ_APIKEY_PUBLIC"),
			os.Getenv("MJ_APIKEY_PRIVATE"),
			"https://api.us.mailjet.com",
		),
		from: &mailjet.RecipientV31{
			Email: "jackeadie@duck.com",
			Name:  "Jack",
		},
	}
}

func (e Emailer) SendEmail(name, email, htmlContent string) error {
	_ = mailjet.MessagesV31{Info: []mailjet.InfoMessagesV31{
		{
			From: e.from,
			To: &mailjet.RecipientsV31{
				mailjet.RecipientV31{
					Email: email,
					Name:  name,
				},
			},
			Subject:  defaultSubject,
			TextPart: defaultTextPart,
			HTMLPart: htmlContent,
		},
	}}
	fmt.Println(htmlContent)
	//_, err := e.client.SendMailV31(&messages)
	return nil
}
