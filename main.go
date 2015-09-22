//--------------------------------------------------------------------------------------------------------------------
//A simple CLI utility that fetches and filters comments of a Hacker News thread.
//Can be used to scrape HN: Who's hiring quickly based on a few keywords
//Uses the HN Api: https://github.com/HackerNews/API
//Caches threads in a .comments directory

//Use with npm's prettyjson

//Todo: Add usage here
//--------------------------------------------------------------------------------------------------------------------
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"html"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/user"
	"strconv"
	"strings"
)

const (
	urlToFormat = "https://hacker-news.firebaseio.com/v0/item/%0.f.json"
)

type hnThread struct {
	Kids []float64 `json:"kids"`
}

type hnComment struct {
	By     string  `json:"by"`
	ID     float64 `json:"id"`
	Parent float64 `json:"parent"`
	Text   string  `json:"text"`
}

type filterFunction func(string) bool

//Fetches contents of a single comment and filters it if any keywords are given based on those
//keywords. If the comment contains these keywords it will be sent to the centralProcess. If no
//keywords are provided all comments are sent to the centralProcess
func getComment(ch chan hnComment, url string) {
	response, err := http.Get(url)
	if err != nil {
		log.Fatalln(err.Error())
	}
	defer response.Body.Close()

	bytes, err := ioutil.ReadAll(response.Body)
	if err != nil {
		log.Fatalln(err.Error())
	}

	hnComm := hnComment{}
	err = json.Unmarshal(bytes, &hnComm)
	if err != nil {
		log.Fatalln(err)
	}

	unescapedText := html.UnescapeString(string(hnComm.Text))
	hnComm.Text = unescapedText
	ch <- hnComm
}

// Fetches all of the comments in a thread
func getThreadFromAPI(url string) *hnThread {
	response, err := http.Get(url)
	if err != nil {
		log.Fatalln(err.Error())
	}
	defer response.Body.Close()

	bytes, err := ioutil.ReadAll(response.Body)
	if err != nil {
		log.Fatalln(err.Error())
	}

	hnThread := &hnThread{}
	err = json.Unmarshal(bytes, hnThread)
	if err != nil {
		log.Fatalln(err.Error())
	}

	return hnThread
}

func fetchFromAPI(threadID float64) []hnComment {

	threadURL := fmt.Sprintf(urlToFormat, threadID)
	thread := getThreadFromAPI(threadURL)

	//WaitGroup to know when all the worker processes finish
	//Channel to communicate between the central process that fetches all the data and the worker processes
	hnCommentChan := make(chan hnComment)

	//Iterate over all comments found and launch a goroutine to fetch it's content
	for _, id := range thread.Kids {
		commentURL := fmt.Sprintf(urlToFormat, id)
		go getComment(hnCommentChan, commentURL)
	}

	var comments []hnComment
	for i := 0; i < len(thread.Kids); i++ {
		c := <-hnCommentChan
		comments = append(comments, c)
	}
	return comments
}

func fetchFromFile(file *os.File) ([]hnComment, error) {
	var hnComments []hnComment
	err := json.NewDecoder(file).Decode(&hnComments)
	if err != nil {
		return nil, err
	}
	return hnComments, nil
}

func filterTextFromKeywords(keywords []string) filterFunction {
	return func(text string) bool {
		lowerText := strings.ToLower(text)
		for _, keyword := range keywords {
			if strings.Contains(lowerText, keyword) {
				return true
			}
		}
		return false
	}
}

func fatalnWrapper(err error) {
	if err != nil {
		log.Fatalln(err)
	}
}

func getCachedFile(fileName string) (*os.File, error) {
	_, err := os.Stat(fileName)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, err
		} else {
			log.Fatalln(err)
		}
	}
	return os.Open(fileName)
}
func fileExists(filename string) bool {
	_, err := os.Stat(filename)
	if err != nil {
		if os.IsNotExist(err) {
			return false
		} else {
			fatalnWrapper(err)
		}
	}
	return true
}

func getComments(threadID int) []hnComment {
	var comments []hnComment
	var err error
	var cachedFile *os.File
	defer cachedFile.Close()

	//This dir is located at ~/
	usr, err := user.Current()
	fatalnWrapper(err)
	defaultDir := usr.HomeDir + "/" + ".cache/hn-article-parser"
	cachedFileName := defaultDir + "/" + strconv.Itoa(threadID) + ".json"

	//If the file exists, read from it otherwise fetch all hncomments and store them
	if fileExists(cachedFileName) {
		log.Println("Reading cached comments from", cachedFileName)
		cachedFile, err = os.Create(cachedFileName)
		fatalnWrapper(err)
		comments, err = fetchFromFile(cachedFile)
		fatalnWrapper(err)
	} else {
		log.Println(fmt.Sprintf("Cachefile %s not found, attempting to fetch threadID: %d",
			cachedFileName, threadID))

		if !fileExists(defaultDir) {
			err := os.MkdirAll(defaultDir, 0777)
			fatalnWrapper(err)
		}
		cachedFile, err = os.Create(cachedFileName)
		fatalnWrapper(err)

		comments = fetchFromAPI(float64(threadID))
		err = json.NewEncoder(cachedFile).Encode(comments)
		fatalnWrapper(err)
	}

	return comments
}

func main() {
	threadID := flag.Int("threadID", 0, "The ID of the HN thread we will use")
	outFileName := flag.String("outFile", "", "Write comments to this file. Defaults to stdout")
	keywordsStr := flag.String("keywords", "",
		"The keywords to filter comments on. Usage -keywords=\"keyword1 keyword2 keyword3\"")
	flag.Parse()

	comments := getComments(*threadID)

	//The output file to write the filtered comments to, defaults to stdout
	var outFile *os.File
	if *outFileName == "" {
		log.Println("No outfile specified, defaulting to stdout")
		outFile = os.Stdout
	} else {
		var err error
		outFile, err = os.Create(*outFileName)
		fatalnWrapper(err)
	}
	defer outFile.Close()

	//If we have no keywords, pipe all to the outfile. Otherwise filter by keywords
	var filter filterFunction
	if len(*keywordsStr) == 0 {
		filter = func(text string) bool {
			return true
		}
	} else {
		filter = filterTextFromKeywords(strings.Split(*keywordsStr, " "))
	}

	filteredComments := make([]hnComment, 0)
	for _, c := range comments {
		if filter(c.Text) {
			filteredComments = append(filteredComments, c)
		}
	}

	//Write json to our outfile if we have any filtered comments
	if len(filteredComments) > 0 {
		if err := json.NewEncoder(outFile).Encode(filteredComments); err != nil {
			log.Fatalln(err)
		}
	} else {
		log.Println("No results found based on the keywords supplied. Not writing outFile")
	}
}
