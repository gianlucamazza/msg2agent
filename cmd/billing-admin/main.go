// billing-admin is a CLI for managing msg2agent billing: tenants, API keys, and usage.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/gianlucamazza/msg2agent/pkg/billing"
)

func usage() {
	fmt.Fprintf(os.Stderr, `billing-admin — msg2agent billing management

Usage:
  billing-admin -db <path> <command> [flags]

Commands:
  create-tenant   Create a new billing tenant
  list-tenants    List all tenants
  suspend-tenant  Suspend a tenant by ID
  issue-key       Issue an API key for a tenant (plaintext printed once)
  revoke-key      Revoke an API key by ID
  list-keys       List API keys for a tenant
  list-usage      Show usage aggregates for a tenant/period
  export-csv      Export usage CSV to stdout
  purge-events    Delete raw audit events older than a date (aggregates preserved)

Flags:
  -db string    Path to billing SQLite database (required)

Run billing-admin -db <path> <command> -help for per-command flags.
`)
}

func main() {
	db := flag.String("db", "", "path to billing SQLite database")
	flag.Usage = usage
	flag.Parse()

	if *db == "" {
		fmt.Fprintln(os.Stderr, "error: -db is required")
		usage()
		os.Exit(1)
	}

	args := flag.Args()
	if len(args) == 0 {
		usage()
		os.Exit(1)
	}

	store, err := billing.NewSQLiteStore(*db)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: open billing db: %v\n", err)
		os.Exit(1)
	}
	defer store.Close()

	cmd := args[0]
	cmdArgs := args[1:]

	switch cmd {
	case "create-tenant":
		runCreateTenant(store, cmdArgs)
	case "list-tenants":
		runListTenants(store)
	case "suspend-tenant":
		runSuspendTenant(store, cmdArgs)
	case "issue-key":
		runIssueKey(store, cmdArgs)
	case "revoke-key":
		runRevokeKey(store, cmdArgs)
	case "list-keys":
		runListKeys(store, cmdArgs)
	case "list-usage":
		runListUsage(store, cmdArgs)
	case "export-csv":
		runExportCSV(store, cmdArgs)
	case "purge-events":
		runPurgeEvents(store, cmdArgs)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", cmd)
		usage()
		os.Exit(1)
	}
}

func runCreateTenant(store billing.Store, args []string) {
	fs := flag.NewFlagSet("create-tenant", flag.ExitOnError)
	name := fs.String("name", "", "tenant display name (required)")
	email := fs.String("email", "", "tenant email (required)")
	plan := fs.String("plan", "free", "plan: free|starter|team|enterprise")
	fs.Parse(args)

	if *name == "" || *email == "" {
		fmt.Fprintln(os.Stderr, "error: -name and -email are required")
		fs.Usage()
		os.Exit(1)
	}

	t := billing.NewTenant(*name, *email, billing.Plan(*plan))
	if err := store.PutTenant(t); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("tenant created\n  ID:    %s\n  Name:  %s\n  Plan:  %s\n", t.ID, t.Name, t.Plan)
}

func runListTenants(store billing.Store) {
	tenants, err := store.ListTenants()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tNAME\tEMAIL\tPLAN\tSTATUS\tCREATED")
	for _, t := range tenants {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			t.ID, t.Name, t.Email, t.Plan, t.Status, t.CreatedAt.Format("2006-01-02"))
	}
	w.Flush()
}

func runSuspendTenant(store billing.Store, args []string) {
	fs := flag.NewFlagSet("suspend-tenant", flag.ExitOnError)
	id := fs.String("id", "", "tenant ID (required)")
	fs.Parse(args)

	if *id == "" {
		fmt.Fprintln(os.Stderr, "error: -id is required")
		os.Exit(1)
	}
	if err := store.SuspendTenant(*id); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("tenant %s suspended\n", *id)
}

func runIssueKey(store billing.Store, args []string) {
	fs := flag.NewFlagSet("issue-key", flag.ExitOnError)
	tenantID := fs.String("tenant", "", "tenant ID (required)")
	name := fs.String("name", "default", "key label")
	ttl := fs.Duration("ttl", 0, "key TTL (e.g. 720h); 0 = no expiry")
	fs.Parse(args)

	if *tenantID == "" {
		fmt.Fprintln(os.Stderr, "error: -tenant is required")
		os.Exit(1)
	}

	plaintext, key, err := billing.GenerateAPIKey(*tenantID, *name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: generate key: %v\n", err)
		os.Exit(1)
	}
	if *ttl > 0 {
		exp := time.Now().UTC().Add(*ttl)
		key.ExpiresAt = &exp
	}
	if err := store.PutAPIKey(key); err != nil {
		fmt.Fprintf(os.Stderr, "error: store key: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("API key issued (shown only once — store it securely):\n\n  %s\n\n", plaintext)
	fmt.Printf("Key ID:  %s\nLabel:   %s\nPrefix:  %s\n", key.ID, key.Name, key.Prefix)
	if key.ExpiresAt != nil {
		fmt.Printf("Expires: %s\n", key.ExpiresAt.Format(time.RFC3339))
	}
}

func runRevokeKey(store billing.Store, args []string) {
	fs := flag.NewFlagSet("revoke-key", flag.ExitOnError)
	id := fs.String("id", "", "key ID (required)")
	fs.Parse(args)

	if *id == "" {
		fmt.Fprintln(os.Stderr, "error: -id is required")
		os.Exit(1)
	}
	if err := store.RevokeAPIKey(*id); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("key %s revoked\n", *id)
}

func runListKeys(store billing.Store, args []string) {
	fs := flag.NewFlagSet("list-keys", flag.ExitOnError)
	tenantID := fs.String("tenant", "", "tenant ID (required)")
	fs.Parse(args)

	if *tenantID == "" {
		fmt.Fprintln(os.Stderr, "error: -tenant is required")
		os.Exit(1)
	}

	keys, err := store.ListAPIKeys(*tenantID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tLABEL\tPREFIX\tSTATUS\tCREATED")
	for _, k := range keys {
		status := "active"
		if k.RevokedAt != nil {
			status = "revoked"
		} else if k.ExpiresAt != nil && time.Now().After(*k.ExpiresAt) {
			status = "expired"
		}
		fmt.Fprintf(w, "%s\t%s\t%s...\t%s\t%s\n",
			k.ID, k.Name, k.Prefix, status, k.CreatedAt.Format("2006-01-02"))
	}
	w.Flush()
}

func runListUsage(store *billing.SQLiteStore, args []string) {
	fs := flag.NewFlagSet("list-usage", flag.ExitOnError)
	period := fs.String("period", "", "billing period YYYY-MM (default: current month)")
	tenantID := fs.String("tenant", "", "filter by tenant ID")
	fs.Parse(args)

	if *period == "" {
		*period = time.Now().UTC().Format("2006-01")
	}

	snaps, err := store.LoadAggregates()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "TENANT\tPERIOD\tEVENT\tCOUNT")
	for _, s := range snaps {
		if *period != "" && s.Period != *period {
			continue
		}
		if *tenantID != "" && s.TenantID != *tenantID {
			continue
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%d\n", s.TenantID, s.Period, string(s.Event), s.Count)
	}
	w.Flush()
}

func runExportCSV(store *billing.SQLiteStore, args []string) {
	fs := flag.NewFlagSet("export-csv", flag.ExitOnError)
	period := fs.String("period", "", "billing period YYYY-MM (empty = all)")
	fs.Parse(args)

	if err := billing.ExportCSV(os.Stdout, *period, store); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func runPurgeEvents(store *billing.SQLiteStore, args []string) {
	fs := flag.NewFlagSet("purge-events", flag.ExitOnError)
	before := fs.String("before", "", "delete events older than this date (YYYY-MM-DD or RFC3339, required)")
	yes := fs.Bool("yes", false, "skip confirmation prompt")
	fs.Parse(args)

	if *before == "" {
		fmt.Fprintln(os.Stderr, "error: -before is required")
		fs.Usage()
		os.Exit(1)
	}

	var cutoff time.Time
	var parseErr error
	for _, layout := range []string{"2006-01-02", time.RFC3339} {
		cutoff, parseErr = time.Parse(layout, *before)
		if parseErr == nil {
			break
		}
	}
	if parseErr != nil {
		fmt.Fprintf(os.Stderr, "error: invalid date %q (use YYYY-MM-DD or RFC3339)\n", *before)
		os.Exit(1)
	}

	if !*yes {
		fmt.Printf("This will permanently delete audit events older than %s.\n", cutoff.Format("2006-01-02"))
		fmt.Print("Type 'yes' to continue: ")
		scanner := bufio.NewScanner(os.Stdin)
		scanner.Scan()
		if strings.TrimSpace(scanner.Text()) != "yes" {
			fmt.Println("aborted")
			os.Exit(0)
		}
	}

	n, err := store.PurgeEvents(cutoff)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("deleted %d audit event(s) before %s (usage_aggregates preserved)\n", n, cutoff.Format("2006-01-02"))
}
