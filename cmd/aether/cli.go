package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"strings"

	"github.com/linell/aether/internal/rest"
	"github.com/linell/aether/internal/store"
)

func restClient(aether string) (*rest.Client, error) {
	token, err := requireToken()
	if err != nil {
		return nil, err
	}
	return &rest.Client{BaseURL: aether, Token: token}, nil
}

func conjure(args []string) error {
	fs := flag.NewFlagSet("conjure", flag.ExitOnError)
	aether := fs.String("aether", "http://127.0.0.1:8080", "aether base URL")
	host := fs.String("host", instanceID(), "host that will run the daemon")
	class := fs.String("class", "anchored", "daemon class: anchored or opportunistic")
	policy := fs.String("policy", "queue", "offline policy: queue, skip, or ttl")
	name, err := parseName(fs, args, "conjure <name> [--host h]")
	if err != nil {
		return err
	}
	client, err := restClient(*aether)
	if err != nil {
		return err
	}
	var doc struct {
		Name string `json:"name"`
		Host string `json:"host"`
	}
	body := map[string]string{"name": name, "host": *host, "class": *class, "offline_policy": *policy}
	if err := client.Do(context.Background(), http.MethodPost, "/v1/daemons", body, &doc); err != nil {
		return err
	}
	fmt.Printf("conjured %s on %s\n", doc.Name, doc.Host)
	return nil
}

func parseName(fs *flag.FlagSet, args []string, usage string) (string, error) {
	if len(args) == 0 || args[0] == "" {
		return "", fmt.Errorf("usage: aether %s", usage)
	}
	if err := fs.Parse(args[1:]); err != nil {
		return "", err
	}
	return args[0], nil
}

func tell(args []string) error {
	fs := flag.NewFlagSet("tell", flag.ExitOnError)
	aether := fs.String("aether", "http://127.0.0.1:8080", "aether base URL")
	name, err := parseName(fs, args, "tell <name> <text>")
	if err != nil {
		return err
	}
	text := strings.Join(fs.Args(), " ")
	if text == "" {
		return errors.New("usage: aether tell <name> <text>")
	}
	client, err := restClient(*aether)
	if err != nil {
		return err
	}
	var doc struct {
		Thread  string `json:"thread"`
		Message string `json:"message"`
	}
	body := map[string]string{"id": store.NewID(), "text": text}
	if err := client.Do(context.Background(), http.MethodPost, "/v1/daemons/"+name+"/messages", body, &doc); err != nil {
		return err
	}
	fmt.Printf("sent %s to %s on thread %s\n", doc.Message, name, doc.Thread)
	return nil
}

func answer(args []string, decision string) error {
	fs := flag.NewFlagSet(decision, flag.ExitOnError)
	aether := fs.String("aether", "http://127.0.0.1:8080", "aether base URL")
	id, err := parseName(fs, args, decision+" <approval-id>")
	if err != nil {
		return err
	}
	client, err := restClient(*aether)
	if err != nil {
		return err
	}
	var doc struct {
		Status string `json:"status"`
		Call   struct {
			Tool string `json:"tool"`
		} `json:"call"`
	}
	body := map[string]string{"decision": decision}
	if err := client.Do(context.Background(), http.MethodPost, "/v1/approvals/"+id+"/answer", body, &doc); err != nil {
		return err
	}
	fmt.Printf("approval %s is %s (%s)\n", id, doc.Status, doc.Call.Tool)
	return nil
}
