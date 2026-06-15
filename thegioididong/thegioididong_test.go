package thegioididong

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newTestClient(srv *httptest.Server) *Client {
	cfg := DefaultConfig()
	cfg.BaseURL = srv.URL
	cfg.Rate = 0
	cfg.Retries = 0
	return NewClientWithConfig(cfg)
}

func sampleProductPageHTML(slug, name, brand string, price float64) string {
	ld, _ := json.Marshal(map[string]any{
		"@type":       "Product",
		"name":        name,
		"description": "Great product",
		"brand":       map[string]string{"name": brand},
		"offers":      map[string]string{"price": "32990000", "priceCurrency": "VND"},
		"aggregateRating": map[string]string{
			"ratingValue": "4.8",
			"reviewCount": "1234",
		},
	})
	return `<!DOCTYPE html><html><head>
<script type="application/ld+json">` + string(ld) + `</script>
</head><body>
<div data-id="99001">` + name + `</div>
</body></html>`
}

func sampleListingHTML(n int) string {
	var sb strings.Builder
	sb.WriteString(`<!DOCTYPE html><html><body><ul>`)
	for i := 0; i < n; i++ {
		sb.WriteString(`<li><a href="/san-pham-so-`)
		sb.WriteString(string(rune('0' + i)))
		sb.WriteString(`.aspx">Product </a></li>`)
	}
	sb.WriteString(`</ul></body></html>`)
	return sb.String()
}

func sampleReviewsJSON(n int) string {
	type r struct {
		CommentID    int64  `json:"commentId"`
		UserName     string `json:"userName"`
		RatePoint    int    `json:"ratePoint"`
		Content      string `json:"content"`
		HelpfulCount int    `json:"helpfulCount"`
	}
	var list []r
	for i := 0; i < n; i++ {
		list = append(list, r{
			CommentID: int64(1000 + i),
			UserName:  "User " + string(rune('A'+i)),
			RatePoint: 5,
			Content:   "Excellent product",
		})
	}
	b, _ := json.Marshal(map[string]any{"data": list})
	return string(b)
}

func TestGet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			t.Error("request carried no User-Agent")
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	body, err := c.Get(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "ok" {
		t.Errorf("body = %q, want ok", body)
	}
}

func TestGetRetriesOn503(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("recovered"))
	}))
	defer srv.Close()

	cfg := DefaultConfig()
	cfg.BaseURL = srv.URL
	cfg.Rate = 0
	cfg.Retries = 5
	c := NewClientWithConfig(cfg)

	start := time.Now()
	body, err := c.Get(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "recovered" {
		t.Errorf("body = %q after retries", body)
	}
	if hits != 3 {
		t.Errorf("server saw %d hits, want 3", hits)
	}
	if time.Since(start) < 500*time.Millisecond {
		t.Error("retries did not back off")
	}
}

func TestGetProduct(t *testing.T) {
	html := sampleProductPageHTML("iphone-16-pro-max", "Apple iPhone 16 Pro Max", "Apple", 32990000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(html))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	p, err := c.GetProduct(context.Background(), "iphone-16-pro-max")
	if err != nil {
		t.Fatal(err)
	}
	if p.Slug != "iphone-16-pro-max" {
		t.Errorf("Slug = %q, want iphone-16-pro-max", p.Slug)
	}
	if p.Name != "Apple iPhone 16 Pro Max" {
		t.Errorf("Name = %q", p.Name)
	}
	if p.Brand != "Apple" {
		t.Errorf("Brand = %q, want Apple", p.Brand)
	}
	if p.Rating != 4.8 {
		t.Errorf("Rating = %v, want 4.8", p.Rating)
	}
	if p.ReviewCount != 1234 {
		t.Errorf("ReviewCount = %d, want 1234", p.ReviewCount)
	}
}

func TestListProducts(t *testing.T) {
	html := `<html><body>
<a href="/iphone-16-pro-max.aspx">iPhone</a>
<a href="/samsung-galaxy-s25.aspx">Samsung</a>
<a href="/oppo-a57.aspx">Oppo</a>
</body></html>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(html))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	products, err := c.ListProducts(context.Background(), "dien-thoai", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(products) != 3 {
		t.Fatalf("got %d products, want 3", len(products))
	}
	if products[0].Slug != "iphone-16-pro-max" {
		t.Errorf("products[0].Slug = %q, want iphone-16-pro-max", products[0].Slug)
	}
}

func TestListProductsDeduplicates(t *testing.T) {
	html := `<html><body>
<a href="/iphone-16-pro-max.aspx">iPhone</a>
<a href="/iphone-16-pro-max.aspx">iPhone duplicate</a>
<a href="/samsung-galaxy-s25.aspx">Samsung</a>
</body></html>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(html))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	products, err := c.ListProducts(context.Background(), "dien-thoai", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(products) != 2 {
		t.Errorf("got %d products after dedup, want 2", len(products))
	}
}

func TestListReviews(t *testing.T) {
	reviews := sampleReviewsJSON(3)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(reviews))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	got, err := c.ListReviews(context.Background(), "99001", "iphone-16-pro-max", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d reviews, want 3", len(got))
	}
	if got[0].ID != "1000" {
		t.Errorf("got[0].ID = %q, want 1000", got[0].ID)
	}
	if got[0].ProductSlug != "iphone-16-pro-max" {
		t.Errorf("got[0].ProductSlug = %q", got[0].ProductSlug)
	}
	if got[0].Rating != 5 {
		t.Errorf("got[0].Rating = %d, want 5", got[0].Rating)
	}
}

func TestListReviewsLimit(t *testing.T) {
	reviews := sampleReviewsJSON(5)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(reviews))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	got, err := c.ListReviews(context.Background(), "99001", "some-slug", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("got %d reviews, want 2 (limit respected)", len(got))
	}
}

func TestParseProductPageNoJSONLD(t *testing.T) {
	html := `<html><body><h1>Some page without JSON-LD</h1></body></html>`
	p := parseProductPage([]byte(html), "some-slug", "https://www.thegioididong.com")
	if p != nil {
		t.Errorf("expected nil for page without JSON-LD, got %+v", p)
	}
}

func TestExtractSlug(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://www.thegioididong.com/iphone-16-pro-max.aspx", "iphone-16-pro-max"},
		{"iphone-16-pro-max.aspx", "iphone-16-pro-max"},
		{"iphone-16-pro-max", "iphone-16-pro-max"},
		{"", ""},
	}
	for _, tc := range cases {
		got := extractSlug(tc.in)
		if got != tc.want {
			t.Errorf("extractSlug(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
