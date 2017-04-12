package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"gopkg.in/alecthomas/kingpin.v2"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"time"
)

//noinspection GoUnusedConst
const (
	ShortenEndpoint = "/api/shorten"
	DeleteEndpoint  = "/api/delete"
	MetaEndpoint    = "/api/meta/" // + code
)

type APIKey string
type Config struct {
	APIKey APIKey
	Server string
}

type ShortenRequest struct {
	Url       string `json:"url"`
	Shortcode string `json:"code,omitempty"`
	Meta      string `json:"meta,omitempty"`
}

type ShortenResponse struct {
	ShortUrl string `json:"short_url"`
}

type DeleteRequest struct {
	Code string `json:"code"`
}

type DeleteResponse struct {
	Code   string `json:"code"`
	Status string `json:"status"`
}

type MetaResponse struct {
	FullUrl string       `json:"full_url"`
	Meta    LinkMetadata `json:"meta"`
}

type LinkMetadata struct {
	Owner    string    `json:"owner"`
	Time     time.Time `json:"time"`
	UserMeta string    `json:"user_meta,omitempty"`
}

type Empty struct{}

var (
	kingpinApp = kingpin.New("pare", "Command-line interface to the Condenser URL shortening service.")
	debugFlag  = kingpinApp.Flag("debug", "Enable debug output.").Bool()
	serverFlag = kingpinApp.Flag("server", "Condenser server URL (overriding on-disk config).").URL()
	apiKeyFlag = kingpinApp.Flag("apikey", "Condenser API key (overriding on-disk config).").String()

	shortenCommand = kingpinApp.Command("shorten", "Shorten a URL.").Alias("short").Default()
	shortcodeArg   = shortenCommand.Flag("code", "Code to shorten to (random if unspecified).").Default("").String()
	metaArg        = shortenCommand.Flag("meta", "User-defined metadata.").Default("").String()
	shortenUrlArg  = shortenCommand.Arg("url", "URL to shorten.").Required().URL()

	rmCommand      = kingpinApp.Command("delete", "Delete a shortcode.").Alias("del").Alias("rm")
	rmShortcodeArg = rmCommand.Arg("code", "Code to delete.").Required().String()
	failNoexistArg = rmCommand.Flag("fail-no-exist", "Return non-zero exit if code didn't exist.").Bool()

	metaCommand = kingpinApp.Command("meta", "Get metadata for a code.")
	metaJsonOut = metaCommand.Flag("json", "Output JSON instead of human-readable.").Bool()
	metaCodeArg = metaCommand.Arg("code", "Code to fetch metadata for.").Required().String()
)

func main() {
	switch kingpin.MustParse(kingpinApp.Parse(os.Args[1:])) {
	case shortenCommand.FullCommand():
		debug("shorten: %s", *shortenUrlArg)
		shorten()
	case rmCommand.FullCommand():
		debug("rm: %s", *rmShortcodeArg)
		rm()
	case metaCommand.FullCommand():
		debug("meta: %s", *metaCodeArg)
		meta()
	}
}

func shorten() {
	bodyStruct := &ShortenRequest{
		Url:       (*shortenUrlArg).String(),
		Shortcode: *shortcodeArg,
		Meta:      *metaArg,
	}
	respStruct := &ShortenResponse{}

	code := doRequest(http.MethodPost, ShortenEndpoint, bodyStruct, respStruct)
	if code == 409 {
		errPrintf("conflict")
		os.Exit(-1)
	} else if code != 200 {
		err := errors.New(fmt.Sprintf("unexpected response code: %v", code))
		kingpin.FatalIfError(err, "unexpected response")
	}

	fmt.Printf("%s\n", respStruct.ShortUrl)
}

func rm() {
	bodyStruct := &DeleteRequest{
		Code: *rmShortcodeArg,
	}
	respStruct := &DeleteResponse{}

	code := doRequest(http.MethodPost, DeleteEndpoint, bodyStruct, respStruct)
	if code != 200 {
		kingpin.Fatalf("unexpected response code: %v", code)
	}

	fmt.Printf("%s/%s\n", respStruct.Code, respStruct.Status)
	if *failNoexistArg && respStruct.Status == "noexist" {
		os.Exit(1)
	}
}

func meta() {
	respStruct := &MetaResponse{}

	code := doRequest(http.MethodGet, MetaEndpoint+*metaCodeArg, Empty{}, respStruct)
	if code == 404 {
		fmt.Println("noexist")
		os.Exit(1)
	} else if code != 200 {
		kingpin.Fatalf("unexpected response code: %v", code)
	}

	if *metaJsonOut {
		// JSON output
		txt, err := json.MarshalIndent(respStruct, "", "  ")
		kingpin.FatalIfError(err, "error marshalling to JSON")
		fmt.Println(string(txt))
	} else {
		// Human output
		fmt.Printf("Code: %s\n", strings.ToUpper(*metaCodeArg))
		fmt.Printf("Full URL: %s\n", respStruct.FullUrl)
		fmt.Printf("Owner: %s\n", respStruct.Meta.Owner)
		fmt.Printf("Created at: %s\n", respStruct.Meta.Time.Format(time.UnixDate))
		if respStruct.Meta.UserMeta != "" {
			fmt.Printf("User-defined metadata: %s\n", respStruct.Meta.UserMeta)
		}
	}
}

func doRequest(method string, endpoint string, body interface{}, response interface{}) int {
	config := serverDetails()
	debug("config: %+v", *config)

	txBody, err := json.Marshal(body)
	kingpin.FatalIfError(err, "error creating shorten POST json")
	debug("txBody: %v", string(txBody))
	req := makeRequest(config, method, endpoint, bytes.NewReader(txBody))
	debug("req: %#v", req)

	client := http.DefaultClient
	resp, err := client.Do(req)
	kingpin.FatalIfError(err, "error executing POST to condenser server")
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return resp.StatusCode
	}

	rxBody, err := ioutil.ReadAll(resp.Body)
	kingpin.FatalIfError(err, "error reading response")
	debug("rxBody: %v", string(rxBody))
	err = json.Unmarshal(rxBody, &response)
	kingpin.FatalIfError(err, "error parsing response")

	return resp.StatusCode
}

func serverDetails() *Config {
	var retUrl string
	var apikey APIKey
	usr, err := user.Current()
	kingpin.FatalIfError(err, "unable to get current user HOME")
	file, err := ioutil.ReadFile(filepath.Join(usr.HomeDir, ".pare.json"))
	if err != nil {
		if os.IsNotExist(err) {
			debug("~/.pare.json doesn't exist; ignoring.")
		} else {
			kingpin.FatalIfError(err, "error opening ~/.pare.json")
		}
	} else {
		var confJson Config
		err := json.Unmarshal(file, &confJson)
		kingpin.FatalIfError(err, "error parsing ~/.pare.json")
		retUrl = confJson.Server
		apikey = confJson.APIKey
	}

	if *serverFlag != nil {
		retUrl = (*serverFlag).String()
	}
	if *apiKeyFlag != "" {
		apikey = APIKey(*apiKeyFlag)
	}
	return &Config{APIKey: apikey, Server: retUrl}
}

func makeRequest(conf *Config, method string, endpoint string, body io.Reader) *http.Request {
	fullUrl := conf.Server + endpoint
	req, err := http.NewRequest(method, fullUrl, body)
	kingpin.FatalIfError(err, "error constructing request")
	req.Header = map[string][]string{
		"Accept":       {"application/json"},
		"Content-Type": {"application/json"},
		"X-API-Key":    {string(conf.APIKey)},
		"User-Agent":   {"Pare (Go net/http)"},
	}
	req.Close = true
	return req
}

func debug(fmat string, objs ...interface{}) {
	if *debugFlag {
		errPrintf(fmat, objs)
	}
}

func errPrintf(fmat string, objs ...interface{}) {
	fmt.Fprintf(os.Stderr, fmat+"\n", objs...)
}
