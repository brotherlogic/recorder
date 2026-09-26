//go:build ignore

package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/brotherlogic/goserver/utils"
	pbdi "github.com/brotherlogic/discovery/proto"
)

func main() {
	conn, err := utils.LFDial(utils.Discover)
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	defer conn.Close()

	registry := pbdi.NewDiscoveryServiceV2Client(conn)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	val, err := registry.RegisterV2(ctx, &pbdi.RegisterRequest{
		Service: &pbdi.RegistryEntry{
			Name:       "recorder",
			Ip:         "192.168.68.108",
			Port:       8087,
			Identifier: "recorder",
		},
	})
	if err != nil {
		log.Fatalf("could not register: %v", err)
	}
	fmt.Printf("Registered: %v\n", val)
}
