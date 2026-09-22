package gh

import (
	"context"
	"fmt"
)

func ExampleNewClient() {
	httpClient, err := NewHTTPClientForDomain(context.Background(), "github.com")
	if err != nil {
		Logger.Errorf("failed to create http client: %s", err.Error())
	}
	client, err := NewClient(httpClient, "github.com")
	if err != nil {
		Logger.Errorf("failed to create github client: %s", err.Error())
	}
	repo, _, err := client.Repositories.Get(context.Background(), "runetes", "maiao")
	if err != nil {
		Logger.Errorf("failed to get github repository: %s", err.Error())
	}
	fmt.Println(repo.GetName())
	// Output: maiao
}
