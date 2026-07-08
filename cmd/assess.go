package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/posit-dev/pev/internal/checks"
	"github.com/posit-dev/pev/internal/discover"
	"github.com/posit-dev/pev/internal/logging"
	"github.com/posit-dev/pev/internal/prompt"
	"github.com/posit-dev/pev/internal/report"
)

// isTerminal returns true when the given file looks like an interactive TTY
// for ANSI colour purposes. NO_COLOR (https://no-color.org/) is honored.
func isTerminal(f *os.File) bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}

func newAssessCmd() *cobra.Command {
	var (
		products       []string
		profile        string
		extraFiles     []string
		includeUser    bool
		nonInteractive bool
		yes            bool
		licenseFile    string
		hostnames      []string
		idp            string
		outputs        []string
		tagsAny        []string
		skipTags       []string
		skipChecks     []string
		reviewSkipped  bool
	)
	c := &cobra.Command{
		Use:   "assess",
		Short: "Run the readiness checks and write Markdown + JSON reports",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Validate the enumerated flags up front so a typo or an
			// unsupported value fails fast with a clear message instead of
			// silently degrading to a green-but-empty report. Normalizing
			// here (ppm -> packagemanager, case-folding) also means the rest
			// of RunE sees canonical catalog tokens.
			normProducts, err := normalizeProducts(products)
			if err != nil {
				return err
			}
			products = normProducts
			normIdP, err := normalizeIdP(idp)
			if err != nil {
				return err
			}
			idp = normIdP
			if err := validateOutputs(outputs); err != nil {
				return err
			}

			outDir, _ := cmd.Flags().GetString("out-dir")
			if outDir == "" {
				outDir = "."
			}
			if err := os.MkdirAll(outDir, 0o755); err != nil {
				return fmt.Errorf("create out-dir: %w", err)
			}

			// Logger
			logFile, err := logging.Init(outDir, mustGetString(cmd, "loglevel"))
			if err != nil {
				return err
			}
			defer logFile.Close()

			parent := cmd.Context()
			if parent == nil {
				parent = context.Background()
			}
			ctx, cancel := context.WithCancel(parent)
			defer cancel()

			started := time.Now()

			// Discovery first — every prompt is seeded from this.
			facts := discover.Gather(ctx)
			log.WithField("facts", facts).Debug("discovery complete")

			// Pick the prompt mode from CLI flags. --non-interactive wins
			// over --yes; both fall through to interactive when neither
			// is set. The interactive driver auto-downgrades to yes when
			// stdin/stdout are not a TTY (CI, piped runs).
			mode := prompt.ModeInteractive
			switch {
			case nonInteractive:
				mode = prompt.ModeNonInteractive
			case yes:
				mode = prompt.ModeYes
			}
			driver := prompt.New(mode)

			// Pre-select the multi-select using the precedence chain:
			//   1. --products flag (explicit user override)
			//   2. --profile flag preset
			//   3. Auto-detect from rstudio-* binaries (DetectProducts)
			//   4. Empty → the "no products" sentinel
			// The multi-select itself runs every time (interactively, an SE
			// confirms or amends the pre-selection; --yes / --non-interactive
			// accept the defaults verbatim per surveyDriver.MultiSelect).
			preselect := computePreselect(products, profile, facts.Products)
			const noneOption = "system configuration checks - product independent"
			// The product-independent system checks always run, so the
			// system-checks box is pre-selected in every case. When a product
			// is detected/requested, BOTH it and the system-checks box start
			// checked (the SE can deselect either). filterNoneOption strips the
			// sentinel afterward; common checks run regardless of the toggle.
			defaults := append([]string{noneOption}, preselect...)
			picks, _ := driver.MultiSelect(
				"Which Posit products will run on this host?",
				[]string{noneOption, "workbench", "connect", "packagemanager"},
				defaults,
			)
			selected := filterNoneOption(picks, noneOption)
			if len(selected) == 0 {
				// Sentinel: no real check uses applies_to.products: [none],
				// so the catalog filter at internal/checks/filter.go drops
				// every product-scoped check and only common checks survive.
				selected = []string{"none"}
			}

			inputs := buildInputs(licenseFile, hostnames, idp, facts, selected, driver)

			// Load catalog.
			extraDirs := []string{}
			if includeUser {
				if home, err := os.UserHomeDir(); err == nil {
					extraDirs = append(extraDirs, filepath.Join(home, ".config", "pev", "checks.d"))
				}
			}
			all, err := checks.Load(checksFS, checksFSRoot, extraFiles, extraDirs)
			if err != nil {
				return fmt.Errorf("load catalog: %w", err)
			}
			if errs := checks.Lint(all); len(errs) > 0 {
				for _, e := range errs {
					log.WithError(e).Error("catalog lint")
				}
				return fmt.Errorf("catalog has %d lint error(s); run `pev lint-checks` for details", len(errs))
			}

			// Filter for selected products + tags.
			f := checks.Filter{
				Products: selected,
				Tags:     tagsAny,
				SkipTags: skipTags,
				SkipIDs:  skipChecks,
			}
			filtered := f.Apply(all)

			// Engine. Progress lines emitted to stderr by the engine
			// itself (see checks.Engine.Run) so users see something
			// happening during long-running checks like apt update or
			// uv venv creation.
			eng := checks.Engine{
				Facts:         facts,
				Inputs:        inputs,
				Progress:      os.Stderr,
				ProgressColor: isTerminal(os.Stderr),
			}
			results := eng.Run(ctx, filtered)

			finished := time.Now()
			rep := checks.Report{
				PevVersion:    buildVersion,
				SchemaVersion: checks.SchemaVersion,
				Host:          facts,
				Run: checks.Run{
					Products: selected,
					Profile:  profile,
					Inputs:   redactSecrets(inputs),
				},
				StartedAt:  started,
				FinishedAt: finished,
				Results:    results,
				Summary:    report.Summarize(results),
			}

			ts := started.UTC().Format("20060102T150405")
			base := fmt.Sprintf("pev-report-%s-%s", facts.Hostname, ts)

			wantMd, wantJSON := wantOutputs(outputs)
			if wantMd {
				p, err := report.WriteMarkdown(outDir, base, rep)
				if err != nil {
					return err
				}
				fmt.Println(p)
			}
			if wantJSON {
				p, err := report.WriteJSON(outDir, base, rep)
				if err != nil {
					return err
				}
				fmt.Println(p)
			}

			// Always print a human-facing summary on stdout. The terminal
			// view shows totals and only failing/unknown checks (grouped
			// by category). The full per-check audit trail — including
			// PASS and SKIP — lives in the on-disk Markdown report.
			fmt.Println()
			report.RenderTerminal(os.Stdout, rep, isTerminal(os.Stdout))

			// --review-skipped appends a skip audit after the failure
			// summary: every SKIPPED check with its reason, grouped by
			// category. Lets an SE confirm a declined prompt or an
			// inapplicable check skipped for the expected reason without
			// opening the on-disk Markdown.
			if reviewSkipped {
				fmt.Println()
				report.RenderSkipped(os.Stdout, rep, isTerminal(os.Stdout))
			}

			return assessExitError(rep.Summary)
		},
	}
	c.Flags().StringSliceVar(&products, "products", nil, "products to assess (workbench,connect,packagemanager; alias ppm); auto-detected if empty")
	c.Flags().StringVar(&profile, "profile", "", "convenience preset: single-server | workbench | connect | ppm")
	c.Flags().StringArrayVar(&extraFiles, "checks-file", nil, "extra YAML check pack (repeatable)")
	c.Flags().BoolVar(&includeUser, "include-user-checks", true, "load packs from ~/.config/pev/checks.d/*.yaml")
	c.Flags().BoolVar(&nonInteractive, "non-interactive", false, "do not prompt; missing inputs become SKIP")
	c.Flags().BoolVar(&yes, "yes", false, "auto-accept discovered defaults")
	c.Flags().StringVar(&licenseFile, "license-file", "", "license file path (overrides discovery)")
	c.Flags().StringSliceVar(&hostnames, "hostnames", nil, "comma-separated key=value pairs: workbench=...,connect=...,ppm=...")
	c.Flags().StringVar(&idp, "idp", "", "identity provider: ldap|saml|oidc|none")
	c.Flags().StringSliceVar(&outputs, "output", []string{"md", "json"}, "comma-separated outputs to write: md,json")
	c.Flags().StringSliceVar(&tagsAny, "tags", nil, "only run checks tagged with ALL of these")
	c.Flags().StringSliceVar(&skipTags, "skip-tags", nil, "skip checks tagged with any of these")
	c.Flags().StringSliceVar(&skipChecks, "skip-checks", nil, "skip checks by ID")
	c.Flags().BoolVar(&reviewSkipped, "review-skipped", false, "after the summary, list skipped checks with their skip reason")
	return c
}

// assessExitError decides the process exit of `pev assess`. The contract
// (plan §4.3) is deliberately narrow: ONLY a non-zero Fail count is
// exit-fatal. WARN is advisory and must never trip a non-zero exit — a
// WARN-only run still exits 0 — and UNKNOWN is historically non-fatal too
// (it renders as a failure on screen but does not gate the exit). Keep this
// keyed solely on Summary.Fail; changing it is a deliberate decision, not a
// drive-by.
func assessExitError(s checks.Summary) error {
	if s.Fail > 0 {
		return fmt.Errorf("%d failure(s) — see report", s.Fail)
	}
	return nil
}

func mustGetString(cmd *cobra.Command, name string) string {
	v, _ := cmd.Flags().GetString(name)
	return v
}

func profileToProducts(profile string) []string {
	switch profile {
	case "single-server":
		return []string{"workbench", "connect", "packagemanager"}
	case "workbench":
		return []string{"workbench"}
	case "connect":
		return []string{"connect"}
	case "ppm":
		return []string{"packagemanager"}
	}
	return nil
}

// computePreselect resolves the multi-select pre-selection per the documented
// precedence chain: explicit --products flag first, then the --profile preset,
// then binary auto-detection. The flag and profile are authoritative when set
// precisely because the SE asked for them — auto-detection is the fallback for
// when neither was given, NOT an override of an explicit request. (A bare
// SelectedFromFlag would consult detection before profile, contradicting the
// doc; this keeps flag > profile > detect.)
func computePreselect(products []string, profile string, detected discover.Products) []string {
	if len(products) > 0 {
		return products
	}
	if profile != "" {
		return profileToProducts(profile)
	}
	return discover.SelectedFromFlag(nil, detected)
}

// canonicalProducts are the catalog's product tokens; --products values
// normalize onto these (or are rejected).
var canonicalProducts = map[string]bool{
	"workbench":      true,
	"connect":        true,
	"packagemanager": true,
}

// normalizeProducts canonicalizes each --products value (case-folded, with the
// ubiquitous `ppm` alias mapped to the catalog's `packagemanager`) and rejects
// any value outside the supported set. Without this, a typo or the documented-
// elsewhere `ppm` alias silently produces a common-only run whose report still
// claims the bogus product was "Selected". Empty/whitespace tokens are dropped
// (cobra emits them for `--products=`), which folds to auto-detect downstream.
func normalizeProducts(in []string) ([]string, error) {
	out := make([]string, 0, len(in))
	for _, raw := range in {
		v := strings.ToLower(strings.TrimSpace(raw))
		switch {
		case v == "":
			continue
		case v == "ppm":
			out = append(out, "packagemanager")
		case canonicalProducts[v]:
			out = append(out, v)
		default:
			return nil, fmt.Errorf("unknown product %q; valid: workbench, connect, packagemanager (alias: ppm)", raw)
		}
	}
	return out, nil
}

// normalizeIdP canonicalizes --idp and rejects unsupported values. The empty
// string (flag unset) is preserved so the interactive opt-in prompt still runs;
// an explicit value must be one of the supported identity-provider kinds.
func normalizeIdP(idp string) (string, error) {
	v := strings.ToLower(strings.TrimSpace(idp))
	switch v {
	case "", "ldap", "saml", "oidc", "none":
		return v, nil
	default:
		return "", fmt.Errorf("unknown idp %q; valid: ldap, saml, oidc, none", idp)
	}
}

// validateOutputs rejects an --output token that names no real format, so a
// typo like `jsn` fails fast instead of silently writing zero report files on
// an exit-0 run. Empty tokens are ignored (cobra emits them for `--output=`,
// which correctly defaults to both formats via wantOutputs).
func validateOutputs(out []string) error {
	for _, raw := range out {
		switch strings.TrimSpace(raw) {
		case "", "md", "markdown", "json":
		default:
			return fmt.Errorf("unknown output %q; valid: md, json", raw)
		}
	}
	return nil
}

// buildInputs collects every input the catalog references: per-product
// hostname/cert/key paths, IdP metadata URL, SMTP host, license file path,
// and the unprivileged user name pev drives renv/uv/pip checks under.
// Order of precedence: explicit CLI flag > prompt answer > discovered default.
// The prompt driver decides at runtime whether to actually show a TUI prompt
// (interactive), silently take the discovered default (yes / non-interactive),
// or downgrade to yes when stdin is not a TTY (e.g. CI, piped runs).
func buildInputs(
	licenseFile string,
	hostnamePairs []string,
	idp string,
	facts discover.HostFacts,
	selected []string,
	d prompt.Driver,
) map[string]string {
	// Pre-seed every catalog-referenced key with the empty string so the
	// engine's `missingkey=error` template option doesn't trip on a YAML
	// `{{ if .Inputs.foo }}` guard for an input the SE didn't supply.
	// Add new keys here when you add a new YAML reference to .Inputs.X.
	in := map[string]string{
		"license_file":       "",
		"unprivileged_user":  "",
		"workbench_hostname": "", "workbench_cert": "", "workbench_key": "",
		"connect_hostname": "", "connect_cert": "", "connect_key": "",
		"ppm_hostname": "", "ppm_cert": "", "ppm_key": "",
		"idp": "none", "idp_metadata_url": "",
		"connect_smtp_host": "",
		"home_share_path":   "",
		"pam_test_user":     "",
		"use_pro_drivers":   "no",
		"postgres_host":     "",
		"postgres_port":     "5432",
	}
	if licenseFile != "" {
		in["license_file"] = licenseFile
	}
	in["unprivileged_user"] = resolveUnprivilegedUser(d)

	defaultHost := facts.FQDN
	if defaultHost == "" {
		defaultHost = facts.Hostname
	}
	hostnameOverrides := parseHostnamePairs(hostnamePairs)
	wanted := selectionToWanted(selected)
	for _, p := range productPrompts() {
		if !wanted[p.key] {
			continue
		}
		promptProductInputs(d, p, in, hostnameOverrides, defaultHost)
	}

	// In common-only mode (no real products selected) every follow-up
	// below targets a product-specific concern — IdP reachability, an
	// external Postgres, PAM/SSO test users, Pro Drivers, an NFS-backed
	// home share. None of them apply to a bare host sanity sweep, so
	// skip the prompts. The dependent common checks SKIP cleanly for
	// missing inputs, which is the right outcome here.
	if wanted["none"] {
		return in
	}

	// IdP reachability isn't workbench-only — Connect (and any product
	// that consumes the customer's IdP) benefits from the same probe.
	// Always offer the prompt; the gate is the confirm inside promptIdP.
	promptIdP(d, idp, in)
	if wanted["connect"] {
		promptSMTP(d, in)
	}
	promptPostgres(d, in)
	promptPAM(d, in)
	promptProDrivers(d, in)
	promptHomeShare(d, in, wanted)
	return in
}

// promptIdP collects the SSO/IdP metadata URL. It runs on every assess
// (any product that consumes the customer's IdP — Workbench and Connect
// today — benefits from the reachability probe). --idp on the CLI
// bypasses the opt-in confirm prompt; idp="none" forces SKIP. Otherwise
// the SE answers a single Yes/No and, if Yes, picks SAML or OIDC.
func promptIdP(d prompt.Driver, idp string, in map[string]string) {
	switch {
	case idp != "" && idp != "none":
		def := defaultIdPMetadataURL(idp)
		got, _ := d.Input("IdP metadata or discovery URL (or 'skip'):", def)
		in["idp"] = idp
		if got != "" {
			in["idp_metadata_url"] = got
		}
	case idp == "none":
		in["idp"] = "none"
	default:
		ssoWant, _ := d.Confirm(
			"Check that your IdP's SAML metadata or OIDC well-known configuration is reachable?",
			false,
		)
		if !ssoWant {
			in["idp"] = "none"
			return
		}
		picked, _ := d.Select("Identity provider type:", []string{"saml", "oidc"}, "saml")
		got, _ := d.Input("IdP metadata or discovery URL (or 'skip'):", defaultIdPMetadataURL(picked))
		in["idp"] = picked
		if got != "" {
			in["idp_metadata_url"] = got
		}
	}
}

// promptSMTP collects the Connect outbound SMTP host. Opt-in, default NO.
// SMTP-reachability is the noisiest false positive on lab hosts where
// outbound 25/587/465 is firewalled by design.
func promptSMTP(d prompt.Driver, in map[string]string) {
	smtpWant, _ := d.Confirm("Check Connect outbound SMTP reachability?", false)
	if !smtpWant {
		return
	}
	got, _ := d.Input("Outbound SMTP host for Connect:", "smtp.example.com")
	in["connect_smtp_host"] = got
}

// promptPostgres collects host/port/db/user/password for an external
// Postgres deployment. We probe network reachability only; SQL
// auth/role/schema validation is the install's job. The password is
// held in memory only — redactSecrets strips it before the inputs map
// lands on disk.
func promptPostgres(d prompt.Driver, in map[string]string) {
	pgWant, _ := d.Confirm("Will this deployment use an external PostgreSQL database?", false)
	if !pgWant {
		return
	}
	host, _ := d.Input("PostgreSQL host:", "")
	if host == "" {
		return
	}
	in["postgres_host"] = host
	port, _ := d.Input("PostgreSQL port:", "5432")
	in["postgres_port"] = port
	if db, _ := d.Input("PostgreSQL database name:", ""); db != "" {
		in["postgres_database"] = db
	}
	if user, _ := d.Input("PostgreSQL username:", ""); user != "" {
		in["postgres_user"] = user
	}
	if pw, _ := d.Password("PostgreSQL password (input hidden):"); pw != "" {
		in["postgres_password"] = pw
	}
}

// promptPAM collects a customer-supplied PAM/SSO test username. Opt-in.
// Catches AD/LDAP/SSSD wired to Workbench PAM that doesn't actually
// resolve through nsswitch.
func promptPAM(d prompt.Driver, in map[string]string) {
	want, _ := d.Confirm("Validate that an SSO/AD test user resolves through PAM/NSS?", false)
	if !want {
		return
	}
	got, _ := d.Input("Test username (must already exist in your IdP):", "")
	if got != "" {
		in["pam_test_user"] = got
	}
}

// promptProDrivers sets a flag indicating Posit Pro Drivers presence
// should be checked. Default NO.
func promptProDrivers(d prompt.Driver, in map[string]string) {
	if want, _ := d.Confirm("Will this deployment use Posit Pro Drivers (rstudio-drivers)?", false); want {
		in["use_pro_drivers"] = "yes"
	}
}

// promptHomeShare collects the local mountpoint of a customer NFS-backed
// share when one is in use. Default NO.
//
// The wording adapts to the selected products: Workbench keeps per-user
// home directories on the share (project files, ~/.config, renv libraries),
// so the question and default mountpoint are about /home. Connect and
// Package Manager have no per-user home requirement — what lives on NFS for
// them is the application's variable-data directory (Connect's
// /var/lib/rstudio-connect, PPM's /var/lib/rstudio-pm) in an HA setup — so
// when Workbench is NOT selected we ask about the DataDir instead. The
// downstream storage.home.* checks are worded generically ("declared home
// share") and probe whatever mountpoint the SE supplies, so only the prompt
// text changes.
func promptHomeShare(d prompt.Driver, in map[string]string, wanted map[string]bool) {
	question := "Will home directory be mounted on an NFS share?"
	inputPrompt := "NFS-backed home share mountpoint (the local path, e.g. /home):"
	defaultPath := "/home"
	if !wanted["workbench"] {
		// Connect/PM only — default the example to whichever product's
		// DataDir applies (Connect wins when both are selected).
		dataDir := "/var/lib/rstudio-connect"
		if wanted["ppm"] && !wanted["connect"] {
			dataDir = "/var/lib/rstudio-pm"
		}
		question = "Will the application's data directory (DataDir) be mounted on an NFS share?"
		inputPrompt = "NFS-backed DataDir mountpoint (the local path, e.g. " + dataDir + "):"
		defaultPath = dataDir
	}
	want, _ := d.Confirm(question, false)
	if !want {
		return
	}
	got, _ := d.Input(inputPrompt, defaultPath)
	if got != "" {
		in["home_share_path"] = got
	}
}

// secretInputKeys lists the input keys that must NEVER land in the JSON
// report or the pev-log. New secret-shaped inputs go here.
var secretInputKeys = map[string]struct{}{
	"postgres_password": {},
}

// redactSecrets returns a copy of in with secret values replaced by
// "(redacted)". The original map keeps the secret because the engine
// still needs it to render template values for the live check (e.g. a
// future postgres primitive that issues a real query). We never persist
// the raw value.
func redactSecrets(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		if _, isSecret := secretInputKeys[k]; isSecret && v != "" {
			out[k] = "(redacted)"
			continue
		}
		out[k] = v
	}
	return out
}

// filterNoneOption strips the "no products" sentinel from a multi-select
// result. Real products win — if the SE ticked workbench alongside the
// sentinel (typical when the menu pre-selected "no products" and the SE
// adds a product without first toggling the sentinel off), the sentinel
// is dropped and only the real products survive. Sentinel-alone returns
// an empty slice; callers detect that via len() and fold it into the
// common-only "none" signal.
func filterNoneOption(picks []string, sentinel string) []string {
	out := make([]string, 0, len(picks))
	for _, p := range picks {
		if p == sentinel {
			continue
		}
		out = append(out, p)
	}
	return out
}

type productPrompt struct{ key, label, defaultCert, defaultKey string }

func productPrompts() []productPrompt {
	return []productPrompt{
		{"workbench", "Workbench", "/etc/rstudio/workbench.crt", "/etc/rstudio/workbench.key"},
		{"connect", "Connect", "/etc/rstudio-connect/connect.crt", "/etc/rstudio-connect/connect.key"},
		{"ppm", "Package Manager", "/etc/rstudio-pm/pm.crt", "/etc/rstudio-pm/pm.key"},
	}
}

// resolveUnprivilegedUser picks the user pev should run renv/uv/pip
// checks as. When the running process is already non-root, that user is
// authoritative. When running as root, prompt with the first /etc/passwd
// human user as the default.
func resolveUnprivilegedUser(d prompt.Driver) string {
	if cu := discover.CurrentNonRootUser(); cu != "" {
		return cu
	}
	def := discover.FirstHumanUser()
	got, _ := d.Input("Unprivileged user to run renv/uv/pip checks as:", def)
	return got
}

func parseHostnamePairs(pairs []string) map[string]string {
	out := map[string]string{}
	for _, kv := range pairs {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || v == "" {
			continue
		}
		out[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	return out
}

func selectionToWanted(selected []string) map[string]bool {
	w := map[string]bool{}
	for _, s := range selected {
		// Catalog uses "packagemanager" but flag/input keys use "ppm".
		switch s {
		case "packagemanager":
			w["ppm"] = true
		default:
			w[s] = true
		}
	}
	return w
}

// promptProductInputs collects hostname + (opt-in) cert/key for one
// product, writing answers into `in`.
func promptProductInputs(d prompt.Driver, p productPrompt, in map[string]string, overrides map[string]string, defaultHost string) {
	def := defaultHost
	if v, ok := overrides[p.key]; ok {
		def = v
	}
	got, _ := d.Input(p.label+" public hostname:", def)
	in[p.key+"_hostname"] = got

	// Cert + key — opt-in to keep the system-check fast path clear.
	// Default is NO (yes / non-interactive accepts the default). When
	// the user opts in we offer a dropdown of candidates discovered
	// inside the product config dir, falling back to a free-form path
	// prompt if nothing was discovered. Once a cert is chosen the
	// matching key prompt is REQUIRED — the x509 primitive needs both
	// halves to verify pairing.
	want, _ := d.Confirm("Check "+p.label+" SSL certificate?", false)
	if !want {
		return
	}
	cands := discover.ScanSSLCandidates(p.key)
	certPath := promptPath(d, p.label+" SSL cert", cands.Certs, p.defaultCert, true)
	if certPath == "" {
		return
	}
	in[p.key+"_cert"] = certPath
	in[p.key+"_key"] = promptPath(d, p.label+" SSL key", cands.Keys, p.defaultKey, false)
}

func defaultIdPMetadataURL(kind string) string {
	switch kind {
	case "saml":
		return "https://idp.example.com/saml/metadata"
	case "oidc":
		return "https://idp.example.com/.well-known/openid-configuration"
	}
	return ""
}

// promptPath shows a Select dropdown of discovered candidates plus a
// "(custom path)" sentinel and (when allowSkip) a "(skip)" sentinel.
// Returns the chosen path, or "" when the SE picked (skip). Falls back to
// a plain Input prompt when no candidates were discovered.
//
// allowSkip=false is for the second half of a cert/key pair: once the SE
// supplies a cert, the partner key MUST come too — the primitive can't
// verify pairing without both halves, so leaving the SE no out beats
// quietly accepting a half-configured prompt.
func promptPath(d prompt.Driver, label string, candidates []string, fallback string, allowSkip bool) string {
	const (
		customSentinel = "(custom path)"
		skipSentinel   = "(skip)"
	)
	inputPrompt := label + " path:"
	if allowSkip {
		inputPrompt = label + " path (or 'skip'):"
	}
	if len(candidates) == 0 {
		got, _ := d.Input(inputPrompt, fallback)
		return got
	}
	options := append([]string{}, candidates...)
	options = append(options, customSentinel)
	if allowSkip {
		options = append(options, skipSentinel)
	}
	pick, _ := d.Select(label+" path:", options, candidates[0])
	switch pick {
	case skipSentinel:
		return ""
	case customSentinel:
		got, _ := d.Input(inputPrompt, fallback)
		return got
	}
	return pick
}

func wantOutputs(out []string) (md, j bool) {
	if len(out) == 0 {
		return true, true
	}
	for _, x := range out {
		switch strings.TrimSpace(x) {
		case "md", "markdown":
			md = true
		case "json":
			j = true
		}
	}
	return md, j
}
