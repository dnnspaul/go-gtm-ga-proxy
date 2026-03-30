package main

import (
	"fmt"
	"net/http"
)

// Dispatcher maps event names to one or more hook functions.
// Each hook receives pointers to the ResponseWriter, Request, HTTP status code,
// and response body, allowing it to inspect or mutate the response in place.
var Dispatcher map[string][]func(*http.ResponseWriter, *http.Request, *int, *[]byte)

// Main is called once at plugin load time. Register all hooks here.
func Main() {
	Dispatcher = make(map[string][]func(*http.ResponseWriter, *http.Request, *int, *[]byte))

	Dispatcher["after_ga_js"] = append(
		Dispatcher["after_ga_js"],
		func(w *http.ResponseWriter, r *http.Request, status *int, body *[]byte) {
			fmt.Println("PLUGIN: executed on event 'after_ga_js'")
			(*w).Header().Add("X-Plugin", "Injected")
		},
	)

	fmt.Println("PLUGIN: loaded successfully")
}

func main() {}
