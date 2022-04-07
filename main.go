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
	users = GetUsersToEmail(users)
	slugs := GetCategorySlugSet(users.Users)
	if len(users.Users) == 0 {
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
	go SendEmails(Emails, ConstructEmailer(), &wg)
	wg.Wait()
}

// GetUsersToEmail calculates who to email based on their EmailFrequency
func GetUsersToEmail(e *EmailList) *EmailList {
	// TODO: Reduce users to those who will receive Emails today. Avoid excess work
	return e
}

// GetCategoryPages calls the dabble api for a list of categories and sends each category along with all the PortfolioSlug(s).
func GetCategoryPages(api *godabble.Api, slugs []CategorySlug, out chan CategoryToPortfolios) {
	defer close(out)
	for _, slug := range slugs {
		c, ps := GetCategoryAndPortfolios(api, slug)
		if len(ps) > 0 {
			out <- CategoryToPortfolios{
				category:   slug,
				portfolios: GetPortfolioSlugs(c),
			}
		}
	}
}

// GetCategoryAndPortfolios retrieves the CategoryPage for a CategorySlug and parse out the relevant PortfolioSlug(s).
func GetCategoryAndPortfolios(api *godabble.Api, s CategorySlug) (*godabble.CategoryPage, []PortfolioSlug) {
	c, err := api.CategoryPage(string(s))
	if err == nil {
		return c, GetPortfolioSlugs(c)
	} else {
		fmt.Println("Error retrieving category page. Error:", err.Error())
		return c, []PortfolioSlug{}
	}
}

// GetPortfolioPages retrieves the PortfolioPage from a list of PortfolioSlug(s) and sends to a channel. Closes out channel.
func GetPortfolioPages(api *godabble.Api, slugs []PortfolioSlug, out chan *godabble.PortfolioPage) {
	defer close(out)
	for _, pSlug := range slugs {
		p, err := api.PortfolioPage(string(pSlug))
		if err == nil {
			out <- p
		} else {
			fmt.Printf("Error retrieving PortfolioPage for %s. Error: %s\n", string(pSlug), err.Error())
		}
	}
}

// AssembleEmailContent constructs EmailContent for a list of EmailSubscriber(s) based on a map of Category -> information. Closes out channel.
func AssembleEmailContent(in []EmailSubscriber, out chan EmailContent, info map[CategorySlug]CategoryToEmailInformation) {
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

// ConstructEmail constructs a HTML email from an EmailContent.
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

// FilterNewsAfter returns the godabble.News that was published after the time.
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
		info.holdings = ReduceHoldings(info.holdings)
		info.news = ReduceNews(info.news)
		m[k] = info
	}
	return m
}

// ReduceNews based on godabble.News.Slug
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

// ReduceHoldings based on godabble.Holding.Slug
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

// ProcessCategoryToPortfolios converts CategoryToPortfolios into a mapping of PortfolioSlug to the CategorySlug(s) that
//  are interested in the Portfolio.
func ProcessCategoryToPortfolios(ins chan CategoryToPortfolios) map[PortfolioSlug][]CategorySlug {
	cpMap := make(map[PortfolioSlug][]CategorySlug)
	for cToP := range ins {
		// Add to portfolio -> category list
		for _, p := range cToP.portfolios {
			_, ok := cpMap[p]
			if !ok {
				cpMap[p] = []CategorySlug{} // make([]CategorySlug)
			}
			cpMap[p] = append(cpMap[p], cToP.category)
		}
	}
	return cpMap
}

// GetPortfolioKeys from a map of PortfolioSlug -> []CategorySlug
func GetPortfolioKeys(cpMap map[PortfolioSlug][]CategorySlug) []PortfolioSlug {
	pChan := make([]PortfolioSlug, len(cpMap))
	i := 0
	for p, _ := range cpMap {
		pChan[i] = p
		i++
	}
	return pChan
}

// GetPortfolioSlugs from the Portfolio within a CategoryPage
func GetPortfolioSlugs(c *godabble.CategoryPage) []PortfolioSlug {
	result := make([]PortfolioSlug, len(c.Portfolios))
	for i, portfolio := range c.Portfolios {
		result[i] = PortfolioSlug(portfolio.Slug)
	}
	return result
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
