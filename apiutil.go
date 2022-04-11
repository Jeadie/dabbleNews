package main

import (
	"fmt"
	"github.com/Jeadie/godabble"
	"log"
)

type CategorySlug string
type PortfolioSlug string

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

// ReduceNews based on godabble.News.Slug
func ReduceNews(n []godabble.News) []godabble.News {
	nn := make(map[string]godabble.News)
	for i, newZ := range n {
		// If exists, remove
		_, ok := nn[newZ.Slug]
		if ok {
			if i >= len(n) {
				n = n[:i]
			} else {
				n = append(n[:i], n[i+1:]...)
			}
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
			if i >= len(h) {
				h = h[:i]
			} else {
				h = append(h[:i], h[i+1:]...)
			}
			// Else, add to map for next time
		} else {
			hh[hold.Slug] = hold
		}
	}
	return h
}

// GetPortfolioSlugs from the Portfolio within a CategoryPage
func GetPortfolioSlugs(c *godabble.CategoryPage) []PortfolioSlug {
	result := make([]PortfolioSlug, len(c.Portfolios))
	for i, portfolio := range c.Portfolios {
		result[i] = PortfolioSlug(portfolio.Slug)
	}
	return result
}

// GetPortfolioKeys from a map of PortfolioSlug -> []CategorySlug
func GetPortfolioKeys(cpMap map[PortfolioSlug][]CategorySlug) []PortfolioSlug {
	pChan := make([]PortfolioSlug, len(cpMap))
	i := 0
	for p, _ := range cpMap {
		pChan[i] = p
		i++
	}
	log.Printf("%d unique portfolio keys\n", len(pChan))
	return pChan
}
