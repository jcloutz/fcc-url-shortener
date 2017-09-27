package main

import (
	"errors"
	"io"
	"log"
	"math/rand"
	"net/url"
	"path"
	"strings"
	"time"

	"fmt"
	"os"

	"net/http"

	"encoding/json"

	"html/template"

	"github.com/dimfeld/httptreemux"
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
)

const chars = "ABCDEFGHIJKLMNOPQRXWYZabcdefghijklmnopqrstuvwxyz1234567890"
const urlCollection = "urls"

// Define the errors for the service
var (
	ErrInvalidURL         = errors.New("Invalid URL Format")
	ErrNotFound           = errors.New("Unable to locate a url with that slug")
	ErrUnableToShortenUrl = errors.New("Unable to create shortened url")
)

// URL is the representation of a url in mongo
type URL struct {
	Slug        string `json:"-" bson:"slug"`
	OriginalURL string `json:"original_url" bson:"original_url"`
	ShortURL    string `json:"short_url" bson:"short_url"`
}

// SlugGenerator generates rand slugs of indeterminate sizes
type SlugGenerator struct {
	random *rand.Rand
}

// JsonError defines the json error response for the service
type JsonError struct {
	Error string `json:"error"`
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		log.Fatal("Port must be set")
	}

	host := os.Getenv("URL_HOST")
	mgoDialString := os.Getenv("URL_MGO_DSN")

	random := rand.New(rand.NewSource(time.Now().Unix()))
	slug := SlugGenerator{random: random}
	sess, err := mgo.Dial(mgoDialString)
	if err != nil {
		log.Fatal(err)
	}

	handlers := Handlers{
		Host:      host,
		masterDB:  sess,
		slugifier: &slug,
	}

	r := httptreemux.New()

	r.GET("/", handlers.Index)
	r.GET("/new/*", handlers.NewURL)
	r.GET("/:slug", handlers.RedirectURL)

	fmt.Printf("Listening on %s\n", host)
	http.ListenAndServe(":"+port, r)
}

// Handlers contains all route handling logic for the service
type Handlers struct {
	Host      string
	masterDB  *mgo.Session
	slugifier *SlugGenerator
}

// Index displays the application instructions
func (h *Handlers) Index(w http.ResponseWriter, r *http.Request, _ map[string]string) {
	file := path.Join("index.html")

	data := struct{ Host string }{Host: h.Host}

	temp, _ := template.ParseFiles(file)
	temp.Execute(w, &data)
}

// NewURL creates a new url in the database
func (h *Handlers) NewURL(w http.ResponseWriter, r *http.Request, params map[string]string) {
	u := params[""]

	if !h.ValidateURL(u) {
		h.RespondError(w, ErrInvalidURL, http.StatusBadRequest)
		return
	}

	reqDB := h.masterDB.Copy()
	defer reqDB.Close()

	collection := reqDB.DB("").C(urlCollection)

	slug := h.slugifier.GenerateUniqueSlug(8, collection, "slug")

	newUrl := URL{
		Slug:        slug,
		OriginalURL: u,
		ShortURL:    h.Host + "/" + slug,
	}

	if err := collection.Insert(&newUrl); err != nil {
		h.RespondError(w, ErrUnableToShortenUrl, http.StatusBadRequest)
		return
	}

	h.RespondJSON(w, newUrl, 201)
}

// RedirectURL parses the url slug and redirects the user to the desired location
func (h *Handlers) RedirectURL(w http.ResponseWriter, r *http.Request, params map[string]string) {
	slug := params["slug"]

	reqDB := h.masterDB.Copy()
	defer reqDB.Close()

	newUrl := URL{}
	if err := reqDB.DB("").C(urlCollection).Find(bson.M{"slug": slug}).One(&newUrl); err != nil {
		h.RespondError(w, ErrNotFound, http.StatusNotFound)

		return
	}

	http.Redirect(w, r, newUrl.OriginalURL, 302)

	return
}

// ValidateURL will check a url to ensure that it is valid
func (h *Handlers) ValidateURL(input string) bool {
	u, err := url.Parse(input)

	fmt.Println(err, u.Scheme, u.Host)
	if err != nil || u.Scheme == "" || !strings.Contains(u.Host, ".") {
		return false
	}

	return true
}

// RespondError creates a valid error response
func (h *Handlers) RespondError(w http.ResponseWriter, err error, status int) {
	h.RespondJSON(w, JsonError{Error: err.Error()}, status)
}

// ResponseJSON handles all json responses from the service
func (h *Handlers) RespondJSON(w http.ResponseWriter, data interface{}, status int) {
	w.Header().Set("Content-Type", "application/json")

	js, err := json.Marshal(data)
	if err != nil {
		js = []byte("{}")
	}

	w.WriteHeader(status)

	io.WriteString(w, string(js))

}

// GenerateSlug will create a random slug of a pre-determined length
func (s *SlugGenerator) GenerateSlug(length int) string {
	slugBytes := make([]byte, length)

	charCount := len(chars) - 1
	for i := 0; i < length; i++ {
		num := s.random.Intn(charCount)
		slugBytes[i] = chars[num]
	}

	slug := string(slugBytes)

	return slug
}

// GenerateUniqueSlug will generate a slug of the specified length and verify that it does not exist
// in the database
func (s *SlugGenerator) GenerateUniqueSlug(length int, c *mgo.Collection, key string) string {
	valid := false
	slug := ""
	for valid == false {
		slug = s.GenerateSlug(length)
		if c, err := c.Find(bson.M{"slug": slug}).Count(); err == nil && c == 0 {
			valid = true
			break
		}
	}

	return slug
}
