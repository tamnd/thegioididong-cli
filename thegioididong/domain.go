package thegioididong

import (
	"context"
	"strings"

	"github.com/tamnd/any-cli/kit"
	"github.com/tamnd/any-cli/kit/errs"
)

func init() { kit.Register(Domain{}) }

// Domain is the TGDD kit driver.
type Domain struct{}

func (Domain) Info() kit.DomainInfo {
	return kit.DomainInfo{
		Scheme: "thegioididong",
		Hosts:  []string{Host},
		Identity: kit.Identity{
			Binary: "thegioididong",
			Short:  "A command line for Thế Giới Di Động.",
			Long: `A command line for Thế Giới Di Động (thegioididong.com).

Fetches product details, category listings, and customer reviews
from Vietnam's largest mobile and electronics retail chain.
No API key required.`,
			Site: "https://" + Host,
			Repo: "https://github.com/tamnd/thegioididong-cli",
		},
	}
}

func (Domain) Register(app *kit.App) {
	app.SetClient(newClient)

	kit.Handle(app, kit.OpMeta{Name: "product", Group: "product", Single: true,
		URIType: "product", Resolver: true, Summary: "Fetch a product by slug or URL",
		Args: []kit.Arg{{Name: "ref", Help: "product slug or URL"}}}, getProduct)

	kit.Handle(app, kit.OpMeta{Name: "products", Group: "product", List: true,
		URIType: "product", Summary: "List products from a category",
		Args: []kit.Arg{{Name: "category", Help: "category slug (e.g. dien-thoai)"}}}, listProducts)

	kit.Handle(app, kit.OpMeta{Name: "reviews", Group: "product", List: true,
		URIType: "review", Summary: "List customer reviews for a product",
		Args: []kit.Arg{{Name: "ref", Help: "product slug or URL"}}}, listReviews)
}

func newClient(_ context.Context, cfg kit.Config) (any, error) {
	c := NewClientWithConfig(DefaultConfig())
	if cfg.UserAgent != "" {
		c.cfg.UserAgent = cfg.UserAgent
	}
	if cfg.Rate > 0 {
		c.cfg.Rate = cfg.Rate
	}
	if cfg.Retries > 0 {
		c.cfg.Retries = cfg.Retries
	}
	if cfg.Timeout > 0 {
		c.cfg.Timeout = cfg.Timeout
		c.http.Timeout = cfg.Timeout
	}
	return c, nil
}

// --- inputs ---

type productRef struct {
	Ref    string  `kit:"arg" help:"product slug or URL"`
	Client *Client `kit:"inject"`
}

type productsIn struct {
	Category string  `kit:"arg" help:"category slug (e.g. dien-thoai)"`
	Limit    int     `kit:"flag,inherit" help:"max results"`
	Client   *Client `kit:"inject"`
}

type reviewsIn struct {
	Ref    string  `kit:"arg" help:"product slug or URL"`
	Limit  int     `kit:"flag,inherit" help:"max results"`
	Client *Client `kit:"inject"`
}

// --- handlers ---

func getProduct(ctx context.Context, in productRef, emit func(*Product) error) error {
	slug := productSlug(in.Ref)
	if slug == "" {
		return errs.Usage("unrecognized TGDD product reference: %q", in.Ref)
	}
	p, err := in.Client.GetProduct(ctx, slug)
	if err != nil {
		return err
	}
	return emit(p)
}

func listProducts(ctx context.Context, in productsIn, emit func(*Product) error) error {
	products, err := in.Client.ListProducts(ctx, in.Category, in.Limit)
	if err != nil {
		return err
	}
	for _, p := range products {
		if err := emit(p); err != nil {
			return err
		}
	}
	return nil
}

func listReviews(ctx context.Context, in reviewsIn, emit func(*Review) error) error {
	slug := productSlug(in.Ref)
	if slug == "" {
		return errs.Usage("unrecognized TGDD product reference: %q", in.Ref)
	}
	base := in.Client.cfg.BaseURL
	if base == "" {
		base = baseURL
	}
	pageURL := base + "/" + slug + ".aspx"
	body, err := in.Client.Get(ctx, pageURL)
	if err != nil {
		return err
	}
	m := dataIdRE.FindSubmatch(body)
	if len(m) < 2 {
		return errs.NotFound("no product ID found for %q", slug)
	}
	productID := string(m[1])
	reviews, err := in.Client.ListReviews(ctx, productID, slug, in.Limit)
	if err != nil {
		return err
	}
	for _, r := range reviews {
		if err := emit(r); err != nil {
			return err
		}
	}
	return nil
}

// --- Resolver ---

func (Domain) Classify(input string) (uriType, id string, err error) {
	slug := productSlug(input)
	if slug != "" {
		return "product", slug, nil
	}
	return "", "", errs.Usage("unrecognized TGDD reference: %q", input)
}

func (Domain) Locate(uriType, id string) (string, error) {
	switch uriType {
	case "product":
		return baseURL + "/" + strings.Trim(id, "/") + ".aspx", nil
	default:
		return "", errs.Usage("thegioididong has no resource type %q", uriType)
	}
}

// productSlug extracts the slug from a URL or returns the bare slug.
func productSlug(input string) string {
	input = strings.TrimSpace(input)
	if strings.Contains(input, "thegioididong.com") || strings.HasPrefix(input, "http") {
		return extractSlug(input)
	}
	slug := strings.TrimSuffix(strings.Trim(input, "/"), ".aspx")
	if slug != "" && !strings.Contains(slug, " ") {
		return slug
	}
	return ""
}
