package main

import (
	"encoding/json"
	"fmt"
	"github.com/Jeadie/godabble"
	"io/ioutil"
	"log"
	"os"
	"sync"
	"time"
)

type EmailFrequency string

const (
	Daily    EmailFrequency = "daily"
	Biweekly                = "biweekly"
	Weekly                  = "weekly"
)

type EnvironmentStage string

const (
	Production EnvironmentStage = "production"
	Beta                        = "beta"
	Local                       = "local"
)

type CategoryToPortfolios struct {
	category   CategorySlug
	portfolios []PortfolioSlug
}

type CategoryToEmailInformation struct {
	category CategorySlug
	news     []godabble.News
	holdings []godabble.Holding
}

type EmailList struct {
	Users []EmailSubscriber `json:"users"`
}

type EmailSubscriber struct {
	Categories []CategorySlug `json:"categories"`
	Email      string         `json:"email"`
	Frequency  EmailFrequency `json:"frequency"`
	Name       string         `json:"name"`
}

const EmailSubscriberJson = "subscribers.json"

func main() {
	api := godabble.Construct()
	users, err := GetEmailList(EmailSubscriberJson)
	if err != nil {
		fmt.Println(err.Error())
		return
	}
	users = GetUsersToEmail(users)
	log.Printf("%d users \n", len(users.Users))
	slugs := GetCategorySlugSet(users.Users)
	log.Printf("%d categories \n", len(slugs))
	if len(users.Users) == 0 {
		log.Println("No users")
		return
	}

	cToPs := make(chan CategoryToPortfolios)
	go GetCategoryPages(api, slugs, cToPs)

	cpMap := ProcessCategoryToPortfolios(cToPs)
	pChan := GetPortfolioKeys(cpMap)

	portfolios := make(chan *godabble.PortfolioPage)
	go GetPortfolioPages(api, pChan, portfolios)
	info := Recombine(cpMap, portfolios)

	// Gather contents for Emails of each User
	Emails := make(chan EmailContent)
	go AssembleEmailContent(users.Users, Emails, info)

	// Send Email
	wg := sync.WaitGroup{}
	wg.Add(1)
	go SendEmails(Emails, ConstructEmailer(GetStage()), &wg)
	wg.Wait()
}

// GetUsersToEmail calculates who to email based on their EmailFrequency
func GetUsersToEmail(e *EmailList) *EmailList {
	e.Users = FilterUsersOnEmailFrequency(e.Users)
	return e
}

func FilterUsersOnEmailFrequency(users []EmailSubscriber) []EmailSubscriber {
	today := time.Now().Weekday()
	for i, u := range users {
		if !ShouldEmailOnDay(today, u.Frequency) {
			users = append(users[:i], users[i+1:]...)
		}
	}
	return users
}

// AssembleEmailContent constructs EmailContent for a list of EmailSubscriber(s) based on a map of Category -> information. Closes out channel.
func AssembleEmailContent(in []EmailSubscriber, out chan EmailContent, info map[CategorySlug]CategoryToEmailInformation) {
	defer close(out)
	for _, u := range in {
		n, h := ConstructUserInformation(u, info)
		out <- EmailContent{
			Email:    u.Email,
			Name:     u.Name,
			News:     n,
			Holdings: h,
		}
	}
}

// SendEmails from a channel of EmailContent using a specific Emailer. Performs Done() on sync.WaitGroup.
func SendEmails(in chan EmailContent, sender *Emailer, wg *sync.WaitGroup) {
	defer wg.Done()
	for e := range in {
		err := sender.SendEmail(
			e.Name,
			e.Email,
			ConstructEmail(e),
		)
		if err != nil {
			fmt.Printf("failed to send Email to %s<%s>. Error: %s\n", e.Name, e.Email, err.Error())
		}
	}
}

// ConstructUserInformation builds the relevant News and Holding(s) for an EmailSubscriber based on their category interests.
func ConstructUserInformation(u EmailSubscriber, info map[CategorySlug]CategoryToEmailInformation) ([]godabble.News, []godabble.Holding) {
	var n []godabble.News
	var h []godabble.Holding
	for _, c := range u.Categories {
		h = append(h, info[c].holdings...)
		n = append(n, info[c].news...)
	}

	n = ReduceNews(n)
	h = ReduceHoldings(h)

	log.Printf("User %s, has %d news before time filtering\n", u.Name, len(n))
	log.Printf("User %s, has %d holdings before time filtering\n", u.Name, len(h))
	// Reduce to those of time relevance
	// Time format "2022-04-05T21:39:16Z"
	// TODO: obey correct time Frequency
	now := time.Now().UTC().Add(-124 * time.Hour)
	news := FilterNewsAfter(n, now)

	log.Printf("User %s, has %d news after time filtering\n", u.Name, len(news))
	log.Printf("User %s, has %d holdings after time filtering\n", u.Name, len(h))

	// Sort Holdings with largest 7Day or 24H difference.
	//if u.Frequency == Daily {
	//	utils.Sort(h, func(a, b interface{}) int {
	//		ha, hb := a.(godabble.Holding), b.(godabble.Holding)
	//		return int(math.Abs(ha.Movement24h) - math.Abs(hb.Movement24h))
	//	})
	//} else {
	//	utils.Sort(h, func(a, b interface{}) int {
	//		ha, hb := a.(godabble.Holding), b.(godabble.Holding)
	//		return int(math.Abs(ha.Movement24h) - math.Abs(hb.Movement24h))
	//	})
	//}

	return news, h
}

// FilterNewsAfter returns the godabble.News that was published after the time.
func FilterNewsAfter(nn []godabble.News, t time.Time) []godabble.News {
	var news []godabble.News
	j := 0
	for _, n := range nn {
		n_t, err := time.Parse(time.RFC3339, n.PublishedOn)
		if err == nil && n_t.After(t) {
			news = append(news, n)
			j++
		}
	}
	return news
}

// Recombine creates a map of CategorySlug to relevant, category-specific, email information (News and Holdings). For
// each Portfolio page, add News and Holdings to each relevant category (based on cpMap). Categories will have multiple
// portfolios, must aggregate News and Holdings.
func Recombine(cpMap map[PortfolioSlug][]CategorySlug, portfolios chan *godabble.PortfolioPage) map[CategorySlug]CategoryToEmailInformation {
	result := make(map[CategorySlug]CategoryToEmailInformation)
	for portfolio := range portfolios {
		categories, ok := cpMap[PortfolioSlug(portfolio.Slug)]
		if !ok {
			continue
		}
		for _, c := range categories {
			r, ok := result[c]
			if !ok {
				result[c] = CategoryToEmailInformation{
					category: c,
					news:     portfolio.News,
					holdings: portfolio.Holdings,
				}
			} else {
				// category exists, append to CategoryToEmailInformation
				r.holdings = append(r.holdings, portfolio.Holdings...)
				r.news = append(r.news, portfolio.News...)
			}
		}
	}
	return Reduce(result)
}

// Reduce the CategoryToEmailInformation to eliminate duplicate Holding and News.
func Reduce(m map[CategorySlug]CategoryToEmailInformation) map[CategorySlug]CategoryToEmailInformation {
	for k, info := range m {
		log.Printf("Category %s has %d holdings before reduction\n", k, len(info.holdings))
		log.Printf("Category %s has %d news before reduction\n", k, len(info.news))
		info.holdings = ReduceHoldings(info.holdings)
		info.news = ReduceNews(info.news)
		m[k] = info
		log.Printf("Category %s has %d holdings after reduction\n", k, len(info.holdings))
		log.Printf("Category %s has %d news after reduction\n", k, len(info.news))
	}
	return m
}

// ProcessCategoryToPortfolios converts CategoryToPortfolios into a mapping of PortfolioSlug to the CategorySlug(s) that
//  are interested in the Portfolio.
func ProcessCategoryToPortfolios(ins chan CategoryToPortfolios) map[PortfolioSlug][]CategorySlug {
	cpMap := make(map[PortfolioSlug][]CategorySlug)
	for cToP := range ins {
		log.Printf("Category %s has %d portfolios\n", cToP.category, len(cToP.portfolios))

		// Add to portfolio -> category list
		for _, p := range cToP.portfolios {
			_, ok := cpMap[p]
			if !ok {
				cpMap[p] = []CategorySlug{}
			}
			cpMap[p] = append(cpMap[p], cToP.category)
		}
	}
	log.Printf("%d Unique portfolios \n", len(cpMap))
	return cpMap
}

// GetEmailList builds an EmailList based on a JSON file.
func GetEmailList(jsonFilePath string) (*EmailList, error) {
	var payload EmailList

	content, err := ioutil.ReadFile(jsonFilePath)
	if err != nil {
		return &payload, fmt.Errorf("failed reading JSON file %s, error: %w", jsonFilePath, err)
	}

	err = json.Unmarshal(content, &payload)
	if err != nil {
		return &payload, fmt.Errorf("failed parsing JSON from file %s, error: %w", jsonFilePath, err)
	}
	return &payload, nil
}

func ShouldEmailOnDay(d time.Weekday, f EmailFrequency) bool {
	switch f {
	case Daily:
		return d == time.Sunday ||
			d == time.Monday ||
			d == time.Tuesday ||
			d == time.Wednesday ||
			d == time.Thursday ||
			d == time.Friday ||
			d == time.Saturday
	case Biweekly:
		return d == time.Sunday || d == time.Wednesday

	case Weekly:
		return d == time.Sunday

	default:
		return false
	}
}

// GetCategorySlugSet gets all unique categories from a list of EmailSubscriber(s).
func GetCategorySlugSet(s []EmailSubscriber) []CategorySlug {

	// Find unique slugs
	slugs := make(map[CategorySlug]int)
	for _, u := range s {
		for _, c := range u.Categories {
			v, ok := slugs[c]
			if !ok {
				slugs[c] = 1
			} else {
				slugs[c] = v + 1
			}
		}
	}

	// Return keys
	j := 0
	result := make([]CategorySlug, len(slugs))
	for c, _ := range slugs {
		result[j] = c
		j++
	}
	return result
}
