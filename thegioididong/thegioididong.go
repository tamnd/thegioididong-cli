// Package thegioididong is the library behind the thegioididong command line:
// the HTTP client, HTML scraping, and typed data models for Thế Giới Di Động
// (thegioididong.com), Vietnam's largest mobile and electronics retail chain.
//
// Product detail pages embed JSON-LD Product schema, which is parsed for core fields.
// Category listing pages use ?pi=N pagination. Reviews are fetched via a JSON AJAX endpoint.
// Product URLs follow the pattern: https://www.thegioididong.com/{slug}.aspx.
package thegioididong

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Host is the canonical site hostname.
const Host = "www.thegioididong.com"

// baseURL is the site root.
const baseURL = "https://www.thegioididong.com"

// DefaultUserAgent mimics a real browser.
const DefaultUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36"

// Config holds the tunable knobs for the HTTP client.
type Config struct {
	BaseURL   string
	Rate      time.Duration
	Retries   int
	Timeout   time.Duration
	UserAgent string
}

// DefaultConfig returns sensible production defaults.
func DefaultConfig() Config {
	return Config{
		BaseURL:   baseURL,
		Rate:      2 * time.Second,
		Retries:   3,
		Timeout:   30 * time.Second,
		UserAgent: DefaultUserAgent,
	}
}

// Client talks to the TGDD website over HTTP.
type Client struct {
	cfg  Config
	http *http.Client
	last time.Time
}

// NewClient returns a Client from DefaultConfig.
func NewClient() *Client { return NewClientWithConfig(DefaultConfig()) }

// NewClientWithConfig returns a Client built from cfg.
func NewClientWithConfig(cfg Config) *Client {
	return &Client{cfg: cfg, http: &http.Client{Timeout: cfg.Timeout}}
}

// Get fetches rawURL and returns the body bytes, pacing and retrying on transient errors.
func (c *Client) Get(ctx context.Context, rawURL string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt <= c.cfg.Retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff(attempt)):
			}
		}
		body, retry, err := c.do(ctx, rawURL)
		if err == nil {
			return body, nil
		}
		lastErr = err
		if !retry {
			return nil, err
		}
	}
	return nil, fmt.Errorf("get %s: %w", rawURL, lastErr)
}

func (c *Client) do(ctx context.Context, rawURL string) ([]byte, bool, error) {
	c.pace()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("User-Agent", c.cfg.UserAgent)
	req.Header.Set("Accept", "text/html,application/json,*/*")
	req.Header.Set("Referer", baseURL+"/")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, true, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return nil, true, fmt.Errorf("http %d", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("http %d", resp.StatusCode)
	}

	b, err := io.ReadAll(resp.Body)
	return b, err != nil, err
}

func (c *Client) pace() {
	if c.cfg.Rate <= 0 {
		return
	}
	if wait := c.cfg.Rate - time.Since(c.last); wait > 0 {
		time.Sleep(wait)
	}
	c.last = time.Now()
}

func backoff(attempt int) time.Duration {
	d := time.Duration(attempt) * 500 * time.Millisecond
	if d > 5*time.Second {
		d = 5 * time.Second
	}
	return d
}

// --- wire JSON-LD types ---

type wireJSONLD struct {
	Type    string           `json:"@type"`
	Name    string           `json:"name"`
	Desc    string           `json:"description"`
	Brand   wireJSONLDBrand  `json:"brand"`
	Offers  wireJSONLDOffer  `json:"offers"`
	Rating  wireJSONLDRating `json:"aggregateRating"`
}

type wireJSONLDBrand struct {
	Name string `json:"name"`
}

type wireJSONLDOffer struct {
	Price    string `json:"price"`
	OldPrice string `json:"oldPrice"`
}

type wireJSONLDRating struct {
	Value       string `json:"ratingValue"`
	ReviewCount string `json:"reviewCount"`
}

// wireReviewList is the AJAX review endpoint response.
type wireReviewList struct {
	Data []wireReview `json:"data"`
}

type wireReview struct {
	ID           int64  `json:"commentId"`
	CustomerName string `json:"userName"`
	Rating       int    `json:"ratePoint"`
	Content      string `json:"content"`
	HelpfulCount int    `json:"helpfulCount"`
	CreatedAt    string `json:"createdDate"`
}

// --- public types ---

// Product is one TGDD product scraped from a detail page.
type Product struct {
	Slug        string  `json:"slug"                    kit:"id" table:"slug"`
	Name        string  `json:"name"                             table:"name"`
	URL         string  `json:"url,omitempty"                    table:"url,url"`
	Price       float64 `json:"price"                            table:"price"`
	OldPrice    float64 `json:"old_price,omitempty"              table:"old_price"`
	Brand       string  `json:"brand,omitempty"                  table:"brand"`
	Description string  `json:"description,omitempty"            table:"-"`
	Rating      float64 `json:"rating,omitempty"                 table:"rating"`
	ReviewCount int     `json:"review_count,omitempty"           table:"reviews"`
	FetchedAt   string  `json:"fetched_at,omitempty"             table:"fetched_at"`
}

// Review is one customer review from the TGDD AJAX endpoint.
type Review struct {
	ID           string `json:"id"                    kit:"id" table:"id"`
	ProductSlug  string `json:"product_slug"                    table:"product_slug"`
	CustomerName string `json:"customer_name,omitempty"         table:"customer_name"`
	Rating       int    `json:"rating"                          table:"rating"`
	Content      string `json:"content,omitempty"               table:"-"`
	HelpfulCount int    `json:"helpful_count,omitempty"         table:"helpful"`
	CreatedAt    string `json:"created_at,omitempty"            table:"created_at"`
	FetchedAt    string `json:"fetched_at,omitempty"            table:"fetched_at"`
}

// --- regexps ---

// jsonLdRE finds JSON-LD script blocks in HTML.
var jsonLdRE = regexp.MustCompile(`(?is)<script[^>]+type="application/ld\+json"[^>]*>([\s\S]*?)</script>`)

// productLinkRE finds product page links in listing HTML.
// TGDD product URLs: /{slug}.aspx
var productLinkRE = regexp.MustCompile(`href="(?:https://www\.thegioididong\.com)?/([a-z0-9][a-z0-9-]+\.aspx)"`)

// dataIdRE extracts numeric product IDs from data-id attributes.
var dataIdRE = regexp.MustCompile(`data-id="(\d+)"`)

// --- client methods ---

// GetProduct fetches a product detail page and parses it.
func (c *Client) GetProduct(ctx context.Context, slug string) (*Product, error) {
	base := c.cfg.BaseURL
	if base == "" {
		base = baseURL
	}
	slug = strings.TrimSuffix(strings.TrimPrefix(slug, "/"), ".aspx")
	pageURL := base + "/" + slug + ".aspx"
	body, err := c.Get(ctx, pageURL)
	if err != nil {
		return nil, fmt.Errorf("product %s: %w", slug, err)
	}
	p := parseProductPage(body, slug, base)
	if p == nil {
		return &Product{Slug: slug, URL: pageURL, FetchedAt: time.Now().UTC().Format(time.RFC3339)}, nil
	}
	return p, nil
}

// ListProducts fetches product listings from a category page.
func (c *Client) ListProducts(ctx context.Context, categorySlug string, limit int) ([]*Product, error) {
	if limit <= 0 {
		limit = 20
	}
	base := c.cfg.BaseURL
	if base == "" {
		base = baseURL
	}
	pageURL := base + "/" + categorySlug
	body, err := c.Get(ctx, pageURL)
	if err != nil {
		return nil, fmt.Errorf("category %s: %w", categorySlug, err)
	}
	return parseListingPage(body, limit, base), nil
}

// ListReviews fetches customer reviews for a product by numeric product ID.
func (c *Client) ListReviews(ctx context.Context, productID string, slug string, limit int) ([]*Review, error) {
	if limit <= 0 {
		limit = 10
	}
	base := c.cfg.BaseURL
	if base == "" {
		base = baseURL
	}
	apiURL := base + "/ajax/Product/ProductComments?productid=" + productID + "&page=1"
	body, err := c.Get(ctx, apiURL)
	if err != nil {
		return nil, fmt.Errorf("reviews for %s: %w", productID, err)
	}
	return parseReviews(body, slug, limit), nil
}

// --- parsers ---

func parseProductPage(body []byte, slug, base string) *Product {
	html := string(body)
	now := time.Now().UTC().Format(time.RFC3339)

	for _, m := range jsonLdRE.FindAllStringSubmatch(html, -1) {
		if len(m) < 2 {
			continue
		}
		var ld wireJSONLD
		if err := json.Unmarshal([]byte(m[1]), &ld); err != nil {
			continue
		}
		if ld.Type != "Product" {
			continue
		}
		price, _ := strconv.ParseFloat(strings.ReplaceAll(ld.Offers.Price, ",", ""), 64)
		oldPrice, _ := strconv.ParseFloat(strings.ReplaceAll(ld.Offers.OldPrice, ",", ""), 64)
		rating, _ := strconv.ParseFloat(ld.Rating.Value, 64)
		reviewCount, _ := strconv.Atoi(ld.Rating.ReviewCount)

		return &Product{
			Slug:        slug,
			Name:        ld.Name,
			URL:         base + "/" + slug + ".aspx",
			Price:       price,
			OldPrice:    oldPrice,
			Brand:       ld.Brand.Name,
			Description: ld.Desc,
			Rating:      rating,
			ReviewCount: reviewCount,
			FetchedAt:   now,
		}
	}
	return nil
}

func parseListingPage(body []byte, limit int, base string) []*Product {
	html := string(body)
	matches := productLinkRE.FindAllStringSubmatch(html, -1)
	seen := map[string]bool{}
	var out []*Product

	for _, m := range matches {
		if len(out) >= limit {
			break
		}
		if len(m) < 2 {
			continue
		}
		slug := strings.TrimSuffix(m[1], ".aspx")
		if seen[slug] {
			continue
		}
		seen[slug] = true
		out = append(out, &Product{
			Slug:      slug,
			URL:       base + "/" + slug + ".aspx",
			FetchedAt: time.Now().UTC().Format(time.RFC3339),
		})
	}
	return out
}

func parseReviews(body []byte, slug string, limit int) []*Review {
	var list wireReviewList
	if err := json.Unmarshal(body, &list); err != nil {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	var out []*Review
	for _, w := range list.Data {
		if len(out) >= limit {
			break
		}
		out = append(out, &Review{
			ID:           strconv.FormatInt(w.ID, 10),
			ProductSlug:  slug,
			CustomerName: w.CustomerName,
			Rating:       w.Rating,
			Content:      w.Content,
			HelpfulCount: w.HelpfulCount,
			CreatedAt:    w.CreatedAt,
			FetchedAt:    now,
		})
	}
	return out
}

// extractSlug extracts the product slug from a TGDD URL or bare slug string.
func extractSlug(rawURL string) string {
	idx := strings.LastIndex(rawURL, "/")
	var slug string
	if idx < 0 {
		slug = rawURL
	} else {
		slug = rawURL[idx+1:]
	}
	slug = strings.TrimSuffix(slug, ".aspx")
	if i := strings.Index(slug, "?"); i >= 0 {
		slug = slug[:i]
	}
	if slug == "" || strings.Contains(slug, ".") {
		return ""
	}
	return slug
}
