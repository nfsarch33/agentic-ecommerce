//go:build ignore
// +build ignore

// gen_facebook_batch_cassette emits a cassette YAML at
// testdata/cassettes/facebook_catalog_batch_100.yaml that simulates
// two META /batch calls of 50 items each, returning 100 successful
// {id: "fb-batch-NNN"} responses in order. Run only when the
// cassette needs regenerating:
//
//	cd internal/adapter/social
//	go run ./testdata/gen_facebook_batch_cassette.go
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

const total = 100
const chunk = 50

func main() {
	var sb strings.Builder
	sb.WriteString("---\nversion: 2\ninteractions:\n")
	for chunkIdx := 0; chunkIdx < total/chunk; chunkIdx++ {
		items := make([]map[string]any, 0, chunk)
		for i := 0; i < chunk; i++ {
			productNum := chunkIdx*chunk + i + 1
			items = append(items, map[string]any{
				"code": 200,
				"body": fmt.Sprintf(`{"id":"fb-batch-%03d"}`, productNum),
			})
		}
		body, err := json.Marshal(items)
		if err != nil {
			panic(err)
		}
		fmt.Fprintf(&sb, `    - id: %d
      request:
        proto: HTTP/1.1
        proto_major: 1
        proto_minor: 1
        content_length: 0
        transfer_encoding: []
        trailer: {}
        host: cassette.facebook.local
        remote_addr: ""
        request_uri: ""
        body: ""
        form: {}
        headers: {}
        url: https://cassette.facebook.local/
        method: POST
      response:
        proto: HTTP/1.1
        proto_major: 1
        proto_minor: 1
        transfer_encoding: []
        trailer: {}
        content_length: %d
        uncompressed: false
        body: |
            %s
        headers:
            Content-Type:
                - application/json
        status: 200 OK
        code: 200
        duration: 1ms
`, chunkIdx, len(body), string(body))
	}
	if err := os.WriteFile("testdata/cassettes/facebook_catalog_batch_100.yaml", []byte(sb.String()), 0o644); err != nil {
		panic(err)
	}
	fmt.Println("wrote testdata/cassettes/facebook_catalog_batch_100.yaml")
}
