// tsz-policy creates, compiles, and activates an immutable policy snapshot.
// It is intentionally small so examples and operators use the same lifecycle
// operations as the ext-proc runtime rather than inserting policy rows by hand.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"

	"thyris-sz/internal/cache"
	"thyris-sz/internal/config"
	"thyris-sz/internal/database"
	"thyris-sz/internal/extproc/policy"
)

func main() {
	name := flag.String("name", "", "route-owned policy name")
	definitionFile := flag.String("file", "", "path to a policy definition JSON file")
	flag.Parse()
	if *name == "" || *definitionFile == "" {
		flag.Usage()
		os.Exit(2)
	}

	definitionBytes, err := os.ReadFile(*definitionFile)
	if err != nil {
		log.Fatalf("read policy definition: %v", err)
	}
	var definition policy.PolicyDefinition
	if err := json.Unmarshal(definitionBytes, &definition); err != nil {
		log.Fatalf("decode policy definition: %v", err)
	}

	config.LoadConfig()
	database.InitDB()
	cache.InitRedis()
	ctx := context.Background()
	sqlDB, err := database.DB.DB()
	if err != nil {
		log.Fatalf("get PostgreSQL connection: %v", err)
	}
	defer sqlDB.Close()

	repository, err := policy.NewPostgresRepository(sqlDB)
	if err != nil {
		log.Fatalf("initialize policy repository: %v", err)
	}
	snapshotID, err := repository.CreateValidated(ctx, *name, definition)
	if err != nil {
		log.Fatalf("validate policy snapshot: %v", err)
	}
	compiler, err := policy.NewCompiler(repository)
	if err != nil {
		log.Fatalf("initialize policy compiler: %v", err)
	}
	if err := compiler.Compile(ctx, snapshotID); err != nil {
		log.Fatalf("compile policy snapshot: %v", err)
	}
	publisher, err := policy.NewRedisActivationPublisher(cache.RDB)
	if err != nil {
		log.Fatalf("initialize policy activation publisher: %v", err)
	}
	activator, err := policy.NewActivator(repository, publisher)
	if err != nil {
		log.Fatalf("initialize policy activator: %v", err)
	}
	if err := activator.Activate(ctx, snapshotID); err != nil {
		log.Fatalf("activate policy snapshot: %v", err)
	}
	snapshot, err := repository.SnapshotByID(ctx, snapshotID)
	if err != nil {
		log.Fatalf("read activated policy snapshot: %v", err)
	}
	fmt.Printf("activated policy=%s version=%d snapshot=%d\n", *name, *snapshot.Version, snapshot.ID)
}
