//--------------------------------------------------------------------------------------------------------------------
//A simple CLI utility that fetches and filters comments of a Hacker News thread.
//Can be used to scrape HN: Who's hiring quickly based on a few keywords
//Uses the HN Api: https://github.com/HackerNews/API

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
	"strconv"
	"strings"
	"sync"
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

//For now just print the message and pipe it to a file...
func centralProcess(ch chan hnComment) {
	for {
		msg, ok := <-ch
		if ok {
			log.Println(msg.Text + "\n")
		} else {
			break
		}
	}

}

//Fetches contents of a single comment and filters it if any keywords are given based on those keywords. If the comment contains
//these keywords it will be sent to the centralProcess. If no keywords are provided all comments are sent to the centralProcess
func getComment(ch chan hnComment, url string, filter filterFunction, wg *sync.WaitGroup) {
	defer wg.Done()

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

	unescapedText := html.UnescapeString(string(hnComm.Text))
	hnComm.Text = unescapedText
	if filter(unescapedText) {
		ch <- hnComm
	}
}

// Fetches all of the comments in a thread
func getAlLCommentIDs(url string) []float64 {
	response, err := http.Get("https://hacker-news.firebaseio.com/v0/item/9996333.json")
	if err != nil {
		log.Fatalln(err.Error())
	}
	defer response.Body.Close()

	bytes, err := ioutil.ReadAll(response.Body)
	if err != nil {
		log.Fatalln(err.Error())
	}

	hnThread := hnThread{}
	err = json.Unmarshal(bytes, &hnThread)
	if err != nil {
		log.Fatalln(err.Error())
	}

	return hnThread.Kids
}

func getFilterFunc(keywords []string) filterFunction {
	var filterFunc filterFunction
	if len(keywords) > 0 {
		filterFunc = func(text string) bool {
			lowerText := strings.ToLower(text)
			for _, keyword := range keywords {
				if strings.Contains(lowerText, keyword) {
					return true
				}
			}
			return false
		}
	} else {
		filterFunc = func(text string) bool {
			return true
		}

	}
	return filterFunc
}

func main() {
	threadID := flag.String("threadID", "", "The ID of the HN thread we will use")
	keywordsStr := flag.String("keywords", "", "The keywords to filter comments on. Usage -keywords=\"keyword1 keyword2 keyword3\"")
	flag.Parse()

	keywords := strings.Split(*keywordsStr, " ")
	urlToFormat := "https://hacker-news.firebaseio.com/v0/item/%0.f.json"

	//Honestly this is a hack to format the string properly. We use floats because the json response is float by default...
	//So we'll adjust the one off string id params instead of adjusting all float json entries
	threadIDFloat, err := strconv.ParseFloat(*threadID, 64)
	if err != nil {
		log.Fatalln(err)
	}
	threadURL := fmt.Sprintf(urlToFormat, threadIDFloat)
	commentIds := getAlLCommentIDs(threadURL)

	//WaitGroup to know when all the worker processes finish
	var wg sync.WaitGroup
	//Channel to communicate between the central process that fetches all the data and the worker processes
	ch := make(chan hnComment)
	filterFunc := getFilterFunc(keywords)

	go centralProcess(ch)

	//Iterate over all comments found and launch a goroutine to fetch it's content
	for _, id := range commentIds {
		commentURL := fmt.Sprintf(urlToFormat, id)

		go getComment(ch, commentURL, filterFunc, &wg)
		wg.Add(1)
	}

	wg.Wait()
	// When the worker processes are done, close that channel to notify the centralProcess that it has nothing more to
	// process
	close(ch)
}
