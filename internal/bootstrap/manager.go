package bootstrap

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/st-mich43l/apexvoid-CRM/integration"
	"github.com/st-mich43l/apexvoid-photobooth/internal/platform"
	"github.com/st-mich43l/apexvoid-photobooth/internal/storage"
)

const (
	applicationID       = "cafe"
	contractVersion     = "v1"
	manifestVersion     = "v1"
	defaultAppVersion   = "0.2.0"
	defaultStateDir     = "/var/lib/apexvoid/bootstrap"
	defaultDatabaseHost = "postgres"
	defaultDatabasePort = 5432
)

type Config struct {
	StateDir         string
	MigrationPath    string
	MigrationDir     string
	AppVersion       string
	MigrationVersion string
	DatabaseHost     string
	DatabasePort     int
	DatabaseSSLMode  string
	PlatformURL      string
	ServiceURL       string
}

type persistedDatabase struct {
	Name                   string `json:"name"`
	Schema                 string `json:"schema"`
	Role                   string `json:"role"`
	Password               string `json:"password"`
	MigrationBundleVersion string `json:"migration_bundle_version"`
}

type persistedState struct {
	Version           int               `json:"version"`
	Phase             string            `json:"phase"`
	SetupCode         string            `json:"setup_code"`
	ServiceCredential string            `json:"service_credential,omitempty"`
	Database          persistedDatabase `json:"database,omitempty"`
}

type manifest struct {
	ManifestVersion string               `json:"manifest_version"`
	Application     manifestApplication  `json:"application"`
	Service         manifestService      `json:"service"`
	Database        manifestDatabase     `json:"database"`
	Permissions     []manifestPermission `json:"permissions"`
	Access          manifestAccess       `json:"access"`
	Migrations      []manifestMigration  `json:"migrations"`
}

type manifestApplication struct {
	ID                 string `json:"id"`
	DisplayName        string `json:"display_name"`
	Description        string `json:"description"`
	Version            string `json:"version"`
	APIContractVersion string `json:"api_contract_version"`
}
type manifestService struct {
	Identity       string `json:"identity"`
	HealthPath     string `json:"health_path"`
	EnrollmentPath string `json:"enrollment_path"`
	FrontendRoute  string `json:"frontend_route"`
	APIRoute       string `json:"api_route"`
}
type manifestDatabase struct {
	Name                   string `json:"name"`
	Schema                 string `json:"schema"`
	Role                   string `json:"role"`
	MigrationBundleVersion string `json:"migration_bundle_version"`
}
type manifestPermission struct {
	// Enterprise's current decoder unmarshals ExternalPermission with the
	// default Go JSON names and DisallowUnknownFields. Keep these wire names
	// compatible with that authoritative implementation.
	Name        string `json:"Name"`
	DisplayName string `json:"DisplayName"`
	Description string `json:"Description"`
	Scope       string `json:"Scope"`
}
type manifestAccess struct {
	Match       string   `json:"match"`
	Permissions []string `json:"permissions"`
}
type manifestMigration struct {
	Version int    `json:"version"`
	Path    string `json:"path"`
	SHA256  string `json:"sha256"`
}

type Manager struct {
	mu             sync.RWMutex
	config         Config
	state          persistedState
	manifestJSON   []byte
	migrationPath  string
	migrationBytes []byte
	migrations     []publishedMigration
	migrationFiles map[string][]byte
	store          *storage.Store
	authorizer     *platform.Client
	db             *sql.DB
}

type publishedMigration struct {
	Version int
	Path    string
	Bytes   []byte
}

func New(config Config) (*Manager, bool, error) {
	if config.StateDir == "" {
		config.StateDir = defaultStateDir
	}
	if config.MigrationDir == "" && config.MigrationPath == "" {
		config.MigrationDir = "db/migrations"
	}
	if config.AppVersion == "" {
		config.AppVersion = defaultAppVersion
	}
	if config.MigrationVersion == "" {
		config.MigrationVersion = "0.2.0"
	}
	if config.DatabaseHost == "" {
		config.DatabaseHost = defaultDatabaseHost
	}
	if config.DatabasePort == 0 {
		config.DatabasePort = defaultDatabasePort
	}
	if config.DatabaseSSLMode == "" {
		config.DatabaseSSLMode = "disable"
	}
	if config.PlatformURL == "" {
		config.PlatformURL = "http://backend:6868"
	}
	if config.ServiceURL == "" {
		config.ServiceURL = "http://cafe:8090"
	}

	if err := os.MkdirAll(config.StateDir, 0700); err != nil {
		return nil, false, err
	}
	_ = os.Chmod(config.StateDir, 0700)
	statePath := filepath.Join(config.StateDir, "state.json")
	state, created, err := loadState(statePath)
	if err != nil {
		return nil, false, err
	}
	migrations, err := discoverMigrations(config)
	if err != nil {
		return nil, false, fmt.Errorf("read migration artifacts: %w", err)
	}
	manifestJSON, migrationPath, err := buildManifest(config, migrations)
	if err != nil {
		return nil, false, err
	}
	files := make(map[string][]byte, len(migrations))
	for _, item := range migrations {
		files[filepath.Base(item.Path)] = append([]byte(nil), item.Bytes...)
	}
	m := &Manager{config: config, state: state, manifestJSON: manifestJSON, migrationPath: migrationPath, migrationBytes: append([]byte(nil), migrations[0].Bytes...), migrations: migrations, migrationFiles: files}
	if created {
		m.state.Phase = "BOOTSTRAP"
		if err := m.saveLocked(); err != nil {
			return nil, false, err
		}
		m.state.Phase = "PENDING_REGISTRATION"
		if err := m.saveLocked(); err != nil {
			return nil, false, err
		}
	}
	return m, created, nil
}

func loadState(path string) (persistedState, bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		code, codeErr := newSecret()
		if codeErr != nil {
			return persistedState{}, false, codeErr
		}
		return persistedState{Version: 1, Phase: "PENDING_REGISTRATION", SetupCode: code}, true, nil
	}
	if err != nil {
		return persistedState{}, false, err
	}
	_ = os.Chmod(path, 0600)
	var state persistedState
	if err := json.Unmarshal(data, &state); err != nil {
		return persistedState{}, false, fmt.Errorf("read bootstrap state: %w", err)
	}
	if state.Version != 1 || state.SetupCode == "" || (state.Phase != "BOOTSTRAP" && state.Phase != "PENDING_REGISTRATION" && state.Phase != "ENROLLING" && state.Phase != "DATABASE_READY" && state.Phase != "ACTIVE") {
		return persistedState{}, false, errors.New("bootstrap state is invalid")
	}
	if state.Phase == "BOOTSTRAP" {
		state.Phase = "PENDING_REGISTRATION"
	}
	return state, false, nil
}

func (m *Manager) saveLocked() error {
	data, err := json.MarshalIndent(m.state, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(m.config.StateDir, ".state-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err = tmp.Chmod(0600); err == nil {
		_, err = tmp.Write(data)
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmpName, filepath.Join(m.config.StateDir, "state.json"))
}

func newSecret() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func discoverMigrations(config Config) ([]publishedMigration, error) {
	if config.MigrationPath != "" {
		body, err := os.ReadFile(config.MigrationPath)
		if err != nil {
			return nil, err
		}
		return []publishedMigration{{Version: migrationNumber(filepath.Base(config.MigrationPath)), Path: filepath.Base(config.MigrationPath), Bytes: body}}, nil
	}
	entries, err := os.ReadDir(config.MigrationDir)
	if err != nil {
		return nil, err
	}
	items := make([]publishedMigration, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".sql" {
			continue
		}
		body, readErr := os.ReadFile(filepath.Join(config.MigrationDir, entry.Name()))
		if readErr != nil {
			return nil, readErr
		}
		items = append(items, publishedMigration{Version: migrationNumber(entry.Name()), Path: entry.Name(), Bytes: body})
	}
	if len(items) == 0 {
		return nil, errors.New("no SQL migration artifacts found")
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Version == items[j].Version {
			return items[i].Path < items[j].Path
		}
		return items[i].Version < items[j].Version
	})
	return items, nil
}

func migrationNumber(name string) int {
	value := 0
	for _, ch := range name {
		if ch < '0' || ch > '9' {
			break
		}
		value = value*10 + int(ch-'0')
	}
	return value
}

func buildManifest(config Config, migrations []publishedMigration) ([]byte, string, error) {
	manifestMigrations := make([]manifestMigration, 0, len(migrations))
	for _, item := range migrations {
		manifestMigrations = append(manifestMigrations, manifestMigration{Version: item.Version, Path: "/.well-known/apexvoid/migrations/" + filepath.Base(item.Path), SHA256: fmt.Sprintf("%x", sha256.Sum256(item.Bytes))})
	}
	m := manifest{
		ManifestVersion: manifestVersion,
		Application:     manifestApplication{ID: applicationID, DisplayName: "ApexVoid Photobooth", Description: "Photo-booth booking and venue operations with an optional café counter", Version: config.AppVersion, APIContractVersion: contractVersion},
		Service:         manifestService{Identity: "cafe-service", HealthPath: "/health", EnrollmentPath: "/.well-known/apexvoid/enroll", FrontendRoute: "/apps/cafe", APIRoute: "/api"},
		Database:        manifestDatabase{Name: "apexvoid_cafe", Schema: "cafe", Role: "apexvoid_cafe", MigrationBundleVersion: config.MigrationVersion},
		Permissions: []manifestPermission{
			{Name: "cafe.catalog.read", DisplayName: "View Café Menu", Description: "View café menu", Scope: "workspace"},
			{Name: "cafe.catalog.manage", DisplayName: "Manage Café Menu", Description: "Create and update café menu items", Scope: "workspace"},
			{Name: "cafe.order.read", DisplayName: "View Café Orders", Description: "View café orders", Scope: "workspace"},
			{Name: "cafe.order.manage", DisplayName: "Manage Café Orders", Description: "Create and fulfill café orders", Scope: "workspace"},
			{Name: "cafe.booking.read", DisplayName: "View Photo Booth Bookings", Description: "View photo booth bookings", Scope: "workspace"},
			{Name: "cafe.booking.manage", DisplayName: "Manage Photo Booth Bookings", Description: "Reserve and manage photo booth sessions", Scope: "workspace"},
			{Name: "cafe.booking.history.read", DisplayName: "View Booking History", Description: "View immutable booking activity history", Scope: "workspace"},
			{Name: "cafe.booking.schedule.manage", DisplayName: "Manage Booking Schedule", Description: "Configure opening hours and booth blackouts", Scope: "workspace"},
			{Name: "cafe.booth.manage", DisplayName: "Configure Photo Booths", Description: "Configure photo-booth stations", Scope: "workspace"},
		},
		Access:     manifestAccess{Match: "any", Permissions: []string{"cafe.catalog.read", "cafe.order.read", "cafe.booking.read", "cafe.booking.history.read", "cafe.booking.schedule.manage"}},
		Migrations: manifestMigrations,
	}
	data, err := json.Marshal(m)
	return data, manifestMigrations[0].Path, err
}

func (m *Manager) Manifest() ([]byte, string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	body := append([]byte(nil), m.manifestJSON...)
	signature, err := integration.SignManifest(m.state.SetupCode, body)
	return body, signature, err
}

func (m *Manager) Migration() (string, []byte) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.migrationPath, append([]byte(nil), m.migrationBytes...)
}

func (m *Manager) MigrationNamed(name string) ([]byte, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	body, ok := m.migrationFiles[filepath.Base(name)]
	return append([]byte(nil), body...), ok
}

func (m *Manager) Phase() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state.Phase
}

// SetupCodeForOperator is used only for the first-start registration banner.
// It is never included in an HTTP response or an application status payload.
func (m *Manager) SetupCodeForOperator() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state.SetupCode
}

func (m *Manager) Ready(ctx context.Context) bool {
	m.mu.RLock()
	active, db := m.state.Phase == "ACTIVE", m.db
	m.mu.RUnlock()
	if !active || db == nil {
		return false
	}
	pingCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	return db.PingContext(pingCtx) == nil
}

func (m *Manager) Snapshot() (*storage.Store, *platform.Client, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.store, m.authorizer, m.state.Phase == "ACTIVE" && m.store != nil && m.authorizer != nil
}

func (m *Manager) Start(ctx context.Context) {
	m.mu.RLock()
	phase := m.state.Phase
	m.mu.RUnlock()
	if phase == "PENDING_REGISTRATION" {
		return
	}
	go m.connectLoop(ctx)
}

func (m *Manager) connectLoop(ctx context.Context) {
	for {
		if _, _, active := m.Snapshot(); active {
			return
		}
		if err := m.activateStored(ctx); err != nil {
			log.Printf("Photobooth database verification pending (phase=%s)", m.Phase())
		}
		timer := time.NewTimer(2 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (m *Manager) activateStored(ctx context.Context) error {
	m.mu.RLock()
	state := m.state
	m.mu.RUnlock()
	if state.Database.Name == "" || state.ServiceCredential == "" {
		return errors.New("enrollment is incomplete")
	}
	return m.activate(ctx, state.Database, state.ServiceCredential)
}

func (m *Manager) Enroll(ctx context.Context, payload []byte, challenge string) (string, error) {
	m.mu.Lock()
	state := m.state
	m.mu.Unlock()
	credentials, err := integration.OpenEnrollment(state.SetupCode, payload)
	if err != nil {
		return "", errors.New("invalid enrollment payload")
	}
	if credentials.ApplicationID != applicationID || credentials.APIContractVersion != contractVersion || len(challenge) < 32 {
		return "", errors.New("enrollment identity does not match this application")
	}
	if credentials.Database.Name != "apexvoid_cafe" || credentials.Database.Schema != "cafe" || credentials.Database.Role != "apexvoid_cafe" || credentials.Database.MigrationBundleVersion != manifestDatabaseVersion(m.manifestJSON) {
		return "", errors.New("enrollment database contract does not match the manifest")
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state.Phase == "ACTIVE" {
		if !sameEnrollment(m.state, credentials) {
			return "", errors.New("enrollment code has already been consumed")
		}
		return integration.SignEnrollmentAcknowledgement(m.state.ServiceCredential, challenge)
	}
	if (m.state.Phase == "ENROLLING" || m.state.Phase == "DATABASE_READY") && !sameEnrollment(m.state, credentials) {
		return "", errors.New("another enrollment is in progress")
	}
	m.state.Phase = "ENROLLING"
	m.state.ServiceCredential = credentials.ServiceCredential
	m.state.Database = persistedDatabase{Name: credentials.Database.Name, Schema: credentials.Database.Schema, Role: credentials.Database.Role, Password: credentials.Database.Password, MigrationBundleVersion: credentials.Database.MigrationBundleVersion}
	if err := m.saveLocked(); err != nil {
		return "", errors.New("could not persist enrollment state")
	}
	// Do not hold the state mutex while opening a network connection.
	database := m.state.Database
	credential := m.state.ServiceCredential
	m.mu.Unlock()
	err = m.activate(ctx, database, credential)
	m.mu.Lock()
	if err != nil {
		return "", errors.New("database verification failed")
	}
	if m.state.Phase != "ACTIVE" {
		m.state.Phase = "ACTIVE"
		if err := m.saveLocked(); err != nil {
			return "", errors.New("could not finalize enrollment state")
		}
	}
	log.Printf("ApexVoid Photobooth enrollment complete; shared database verified")
	return integration.SignEnrollmentAcknowledgement(credential, challenge)
}

func sameEnrollment(state persistedState, credentials integration.EnrollmentCredentials) bool {
	return subtle.ConstantTimeCompare([]byte(state.ServiceCredential), []byte(credentials.ServiceCredential)) == 1 && state.Database.Name == credentials.Database.Name && state.Database.Schema == credentials.Database.Schema && state.Database.Role == credentials.Database.Role && subtle.ConstantTimeCompare([]byte(state.Database.Password), []byte(credentials.Database.Password)) == 1
}

func manifestDatabaseVersion(raw []byte) string {
	var value struct {
		Database struct {
			MigrationBundleVersion string `json:"migration_bundle_version"`
		} `json:"database"`
	}
	_ = json.Unmarshal(raw, &value)
	return value.Database.MigrationBundleVersion
}

func (m *Manager) activate(ctx context.Context, database persistedDatabase, credential string) error {
	db, err := m.openDatabase(ctx, database)
	if err != nil {
		return err
	}
	if err = verifyDatabase(ctx, db, database); err != nil {
		_ = db.Close()
		return err
	}
	authorizer, err := platform.New(m.config.PlatformURL, applicationID, credential)
	if err != nil {
		_ = db.Close()
		return err
	}
	store := storage.New(db)
	m.mu.Lock()
	if m.db != nil && m.db != db {
		_ = m.db.Close()
	}
	m.db, m.store, m.authorizer = db, store, authorizer
	m.state.Phase = "DATABASE_READY"
	if err := m.saveLocked(); err != nil {
		_ = db.Close()
		m.db, m.store, m.authorizer = nil, nil, nil
		m.state.Phase = "ENROLLING"
		m.mu.Unlock()
		return err
	}
	m.state.Phase = "ACTIVE"
	if err := m.saveLocked(); err != nil {
		_ = db.Close()
		m.db, m.store, m.authorizer = nil, nil, nil
		m.state.Phase = "DATABASE_READY"
		m.mu.Unlock()
		return err
	}
	m.mu.Unlock()
	return nil
}

func (m *Manager) openDatabase(ctx context.Context, database persistedDatabase) (*sql.DB, error) {
	query := url.Values{"sslmode": []string{m.config.DatabaseSSLMode}, "search_path": []string{database.Schema}}
	dsn := (&url.URL{Scheme: "postgres", User: url.UserPassword(database.Role, database.Password), Host: net.JoinHostPort(m.config.DatabaseHost, strconv.Itoa(m.config.DatabasePort)), Path: "/" + database.Name, RawQuery: query.Encode()}).String()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err = db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func verifyDatabase(ctx context.Context, db *sql.DB, database persistedDatabase) error {
	verifyCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var currentDB, currentUser string
	var tables [5]string
	err := db.QueryRowContext(verifyCtx, `SELECT current_database(), current_user, COALESCE(to_regclass('cafe.cafe_items')::text,''), COALESCE(to_regclass('cafe.cafe_orders')::text,''), COALESCE(to_regclass('cafe.cafe_order_lines')::text,''), COALESCE(to_regclass('cafe.cafe_booths')::text,''), COALESCE(to_regclass('cafe.cafe_bookings')::text,'')`).Scan(&currentDB, &currentUser, &tables[0], &tables[1], &tables[2], &tables[3], &tables[4])
	if err != nil {
		return err
	}
	if currentDB != database.Name || currentUser != database.Role {
		return errors.New("database identity mismatch")
	}
	for _, table := range tables {
		if table == "" {
			return errors.New("database migration is incomplete")
		}
	}
	return nil
}
