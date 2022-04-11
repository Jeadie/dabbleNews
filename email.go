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
	stage  EnvironmentStage
}

func ConstructEmailer(stage EnvironmentStage) *Emailer {
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
		stage: stage,
	}
}

func (e Emailer) SendEmail(name, email, htmlContent string) error {
	messages := mailjet.MessagesV31{Info: []mailjet.InfoMessagesV31{
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
	if e.stage == Production {
		_, err := e.client.SendMailV31(&messages)
		return err
	} else {
		fmt.Println("Not in production. Email will not be sent. Content is below.")
		fmt.Println(htmlContent)
		return nil
	}
}
