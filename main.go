package main

import (
	"encoding/json"
	"fmt"
	"github.com/Jeadie/godabble"
	"io/ioutil"
	"strings"
	"sync"
	"time"
)

type EmailFrequency string

const (
	Daily    EmailFrequency = "daily"
	Biweekly                = "biweekly"
	weekly                  = "weekly"
)

type CategorySlug string
type PortfolioSlug string

type CategoryToPortfolios struct {
	category   CategorySlug
	portfolios []PortfolioSlug
}

type CategoryToEmailInformation struct {
	category CategorySlug
	news     []godabble.News
	holdings []godabble.Holding
}

type EmailContent struct {
	Email    string
	Name     string
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
	// TODO: Reduce users to those who will receive Emails today. Avoid excess work
	//users := GetUsersToEmail(users)
	slugs := GetCategorySlugSet(users.Users)
	if len(users.Users) == 0 {
		return
	}

	cToPs := make(chan CategoryToPortfolios)
	go func(api *godabble.Api, slugs []CategorySlug, out chan CategoryToPortfolios) {
		defer close(out)
		for _, slug := range slugs {
			c, err := api.CategoryPage(string(slug))
			if err == nil {
				out <- CategoryToPortfolios{
					category:   slug,
					portfolios: GetPortfolioSlugs(c),
				}
			} else {
				fmt.Println("Error retrieving category page. Error:", err.Error())
			}
		}
	}(api, slugs, cToPs)

	cpMap, pChan := ProcessCategoryToPortfolios(cToPs)
	portfolios := make(chan *godabble.PortfolioPage)
	go func(api *godabble.Api, slugs []PortfolioSlug, out chan *godabble.PortfolioPage) {
		defer close(out)
		for _, pSlug := range slugs {
			p, err := api.PortfolioPage(string(pSlug))
			if err == nil {
				out <- p
			} else {
				fmt.Printf("Error retrieving PortfolioPage for %s. Error: %s\n", string(pSlug), err.Error())
			}
		}
	}(api, pChan, portfolios)
	info := Recombine(cpMap, portfolios)

	// Gather contents for Emails of each User
	Emails := make(chan EmailContent)
	go func(in []EmailSubscriber, out chan EmailContent, info map[CategorySlug]CategoryToEmailInformation) {
		defer close(out)
		for _, u := range in {
			n, h := ConstructUserInformation(u, info)
			out <- EmailContent{
				Email:    u.Email,
				Name:     u.Name,
				news:     n,
				holdings: h,
			}
		}
	}(users.Users, Emails, info)

	// Send Email
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func(in chan EmailContent, sender *Emailer, wg *sync.WaitGroup) {
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

	}(Emails, ConstructEmailer(), &wg)
	wg.Wait()
}

func ConstructEmail(content EmailContent) string {
	lines := make([]string, len(content.news)+len(content.holdings))
	for i, n := range content.news {
		if len(n.Slug) == 0 {
			i--
			continue
		}
		lines[i] = fmt.Sprintf("<li> %s: <a href='https://dabble.com%s>Read more</a> <li>", n.Title, n.Slug)
	}
	for i, h := range content.holdings {
		lines[i+len(content.news)] = fmt.Sprintf(
			"<li> Holding %s. 24 hour movement %2.2f, 7 Day movement %2.2f <li>",
			h.Title, h.Movement24h, h.Movement7d,
		)
	}
	return fmt.Sprintf("<html>Welcome %s, here's your news\n <ul>%s</ul></html>", content.Name, strings.Join(lines, "\n"))
}

func ConstructUserInformation(u EmailSubscriber, info map[CategorySlug]CategoryToEmailInformation) ([]godabble.News, []godabble.Holding) {
	var n []godabble.News
	var h []godabble.Holding
	for _, c := range u.Categories {
		h = append(h, info[c].holdings...)
		n = append(n, info[c].news...)
	}

	n = ReduceNews(n)
	h = ReduceHoldings(h)

	// Reduce to those of time relevance
	// Time format "2022-04-05T21:39:16Z"
	// TODO: obey correct time Frequency
	now := time.Now().Add(-24 * time.Hour)
	news := FilterNewsAfter(n, now)

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

func FilterNewsAfter(nn []godabble.News, t time.Time) []godabble.News {
	news := make([]godabble.News, len(nn))
	j := 0
	for _, n := range nn {
		n_t, err := time.Parse(time.RFC3339, n.PublishedOn)
		if err == nil && n_t.After(t) {
			news[j] = n
			j++
		}
	}
	return news
}

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

func Reduce(m map[CategorySlug]CategoryToEmailInformation) map[CategorySlug]CategoryToEmailInformation {
	for k, info := range m {
		m[k] = *ReduceCategoryToEmailInformation(&info)
	}
	return m
}

func ReduceCategoryToEmailInformation(c *CategoryToEmailInformation) *CategoryToEmailInformation {
	c.holdings = ReduceHoldings(c.holdings)
	c.news = ReduceNews(c.news)
	return c
}

func ReduceNews(n []godabble.News) []godabble.News {
	nn := make(map[string]godabble.News)
	for i, newZ := range n {
		// If exists, remove
		_, ok := nn[newZ.Slug]
		if ok {
			n = append(n[:i], n[i+1:]...)
			// Else, add to map for next time
		} else {
			nn[newZ.Slug] = newZ
		}
	}
	return n
}

func ReduceHoldings(h []godabble.Holding) []godabble.Holding {
	hh := make(map[string]godabble.Holding)
	for i, hold := range h {
		// If exists, remove
		_, ok := hh[hold.Slug]
		if ok {
			h = append(h[:i], h[i+1:]...)
			// Else, add to map for next time
		} else {
			hh[hold.Slug] = hold
		}
	}
	return h
}

func ProcessCategoryToPortfolios(ins chan CategoryToPortfolios) (map[PortfolioSlug][]CategorySlug, []PortfolioSlug) {
	cpMap := make(map[PortfolioSlug][]CategorySlug)
	var pChan []PortfolioSlug
	for cToP := range ins {
		// Add to portfolio -> category list
		for _, p := range cToP.portfolios {
			_, ok := cpMap[p]
			if !ok {
				cpMap[p] = []CategorySlug{} // make([]CategorySlug)
				pChan = append(pChan, p)
			}
			cpMap[p] = append(cpMap[p], cToP.category)
		}
	}
	return cpMap, pChan
}

func GetPortfolioSlugs(c *godabble.CategoryPage) []PortfolioSlug {
	result := make([]PortfolioSlug, len(c.Portfolios))
	for i, portfolio := range c.Portfolios {
		result[i] = PortfolioSlug(portfolio.Slug)
	}
	return result
}

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
	// return slugs.keys()
	j := 0
	result := make([]CategorySlug, len(slugs))
	for c, _ := range slugs {
		result[j] = c
		j++
	}
	return result
}
