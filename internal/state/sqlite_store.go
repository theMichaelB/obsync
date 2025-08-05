package state

import (
	"database/sql"
	"fmt"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/TheMichaelB/obsync/internal/events"
	"github.com/TheMichaelB/obsync/internal/models"
)

// SQLiteStore implements SQLite-based state storage.
type SQLiteStore struct {
	db     *sql.DB
	logger *events.Logger

	// Locking
	mu    sync.RWMutex
	locks map[string]*sync.Mutex
}

// NewSQLiteStore creates a SQLite state store.
func NewSQLiteStore(dbPath string, logger *events.Logger) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite3", dbPath+"?_journal=WAL&_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	store := &SQLiteStore{
		db:     db,
		logger: logger.WithField("component", "sqlite_state_store"),
		locks:  make(map[string]*sync.Mutex),
	}

	if err := store.initialize(); err != nil {
		db.Close()
		return nil, fmt.Errorf("initialize database: %w", err)
	}

	return store, nil
}

// initialize creates tables and indexes.
func (s *SQLiteStore) initialize() error {
	schema := `
    CREATE TABLE IF NOT EXISTS sync_states (
        vault_id TEXT PRIMARY KEY,
        version INTEGER NOT NULL DEFAULT 0,
        last_sync_time TIMESTAMP,
        last_error TEXT,
        created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
        updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
    );
    
    CREATE TABLE IF NOT EXISTS sync_files (
        vault_id TEXT NOT NULL,
        path TEXT NOT NULL,
        hash TEXT NOT NULL,
        updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
        PRIMARY KEY (vault_id, path),
        FOREIGN KEY (vault_id) REFERENCES sync_states(vault_id) ON DELETE CASCADE
    );
    
    CREATE INDEX IF NOT EXISTS idx_sync_files_vault ON sync_files(vault_id);
    
    CREATE TABLE IF NOT EXISTS schema_info (
        version INTEGER PRIMARY KEY
    );
    
    INSERT OR IGNORE INTO schema_info (version) VALUES (?);
    `

	if _, err := s.db.Exec(schema, CurrentSchemaVersion); err != nil {
		return fmt.Errorf("create schema: %w", err)
	}

	// Enable foreign keys
	if _, err := s.db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		return fmt.Errorf("enable foreign keys: %w", err)
	}

	return nil
}

// Load retrieves state from database.
func (s *SQLiteStore) Load(vaultID string) (*models.SyncState, error) {
	s.logger.WithField("vault_id", vaultID).Debug("Loading state from SQLite")

	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Load main state
	var state models.SyncState
	var lastSyncTime sql.NullTime
	var lastError sql.NullString

	err = tx.QueryRow(`
        SELECT version, last_sync_time, last_error
        FROM sync_states
        WHERE vault_id = ?
    `, vaultID).Scan(&state.Version, &lastSyncTime, &lastError)

	if err == sql.ErrNoRows {
		return nil, ErrStateNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query state: %w", err)
	}

	state.VaultID = vaultID
	if lastSyncTime.Valid {
		state.LastSyncTime = lastSyncTime.Time
	}
	if lastError.Valid {
		state.LastError = lastError.String
	}

	// Load files
	state.Files = make(map[string]string)

	rows, err := tx.Query(`
        SELECT path, hash
        FROM sync_files
        WHERE vault_id = ?
    `, vaultID)
	if err != nil {
		return nil, fmt.Errorf("query files: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var path, hash string
		if err := rows.Scan(&path, &hash); err != nil {
			return nil, fmt.Errorf("scan file row: %w", err)
		}
		state.Files[path] = hash
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate files: %w", err)
	}

	return &state, nil
}

// Save persists state to database.
func (s *SQLiteStore) Save(vaultID string, state *models.SyncState) error {
	s.logger.WithFields(map[string]interface{}{
		"vault_id": vaultID,
		"version":  state.Version,
		"files":    len(state.Files),
	}).Debug("Saving state to SQLite")

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Upsert main state
	_, err = tx.Exec(`
        INSERT INTO sync_states (vault_id, version, last_sync_time, last_error, updated_at)
        VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
        ON CONFLICT(vault_id) DO UPDATE SET
            version = excluded.version,
            last_sync_time = excluded.last_sync_time,
            last_error = excluded.last_error,
            updated_at = CURRENT_TIMESTAMP
    `, vaultID, state.Version, state.LastSyncTime, state.LastError)

	if err != nil {
		return fmt.Errorf("upsert state: %w", err)
	}

	// Delete removed files
	_, err = tx.Exec("DELETE FROM sync_files WHERE vault_id = ?", vaultID)
	if err != nil {
		return fmt.Errorf("delete old files: %w", err)
	}

	// Insert files in batches
	const batchSize = 100
	stmt, err := tx.Prepare(`
        INSERT INTO sync_files (vault_id, path, hash)
        VALUES (?, ?, ?)
    `)
	if err != nil {
		return fmt.Errorf("prepare statement: %w", err)
	}
	defer stmt.Close()

	batch := 0
	for path, hash := range state.Files {
		if _, err := stmt.Exec(vaultID, path, hash); err != nil {
			return fmt.Errorf("insert file %s: %w", path, err)
		}

		batch++
		if batch >= batchSize {
			// Commit and start new transaction for large file sets
			if err := tx.Commit(); err != nil {
				return fmt.Errorf("commit batch: %w", err)
			}

			tx, err = s.db.Begin()
			if err != nil {
				return fmt.Errorf("begin new transaction: %w", err)
			}

			stmt.Close()
			stmt, err = tx.Prepare(`
                INSERT INTO sync_files (vault_id, path, hash)
                VALUES (?, ?, ?)
            `)
			if err != nil {
				return fmt.Errorf("prepare statement: %w", err)
			}

			batch = 0
		}
	}

	return tx.Commit()
}

// Reset removes state for a vault.
func (s *SQLiteStore) Reset(vaultID string) error {
	s.logger.WithField("vault_id", vaultID).Info("Resetting state in SQLite")

	_, err := s.db.Exec("DELETE FROM sync_states WHERE vault_id = ?", vaultID)
	if err != nil {
		return fmt.Errorf("delete state: %w", err)
	}

	return nil
}

// List returns all vault IDs.
func (s *SQLiteStore) List() ([]string, error) {
	rows, err := s.db.Query("SELECT vault_id FROM sync_states ORDER BY vault_id")
	if err != nil {
		return nil, fmt.Errorf("query vaults: %w", err)
	}
	defer rows.Close()

	var vaultIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan vault ID: %w", err)
		}
		vaultIDs = append(vaultIDs, id)
	}

	return vaultIDs, rows.Err()
}

// Lock acquires a lock for a vault.
func (s *SQLiteStore) Lock(vaultID string) (UnlockFunc, error) {
	s.mu.Lock()
	lock, exists := s.locks[vaultID]
	if !exists {
		lock = &sync.Mutex{}
		s.locks[vaultID] = lock
	}
	s.mu.Unlock()

	// Try to acquire lock with timeout
	done := make(chan struct{})
	go func() {
		lock.Lock()
		close(done)
	}()

	select {
	case <-done:
		return func() { lock.Unlock() }, nil
	case <-time.After(5 * time.Second):
		return nil, ErrStateLocked
	}
}

// Migrate is implemented in JSONStore.
func (s *SQLiteStore) Migrate(target Store) error {
	return fmt.Errorf("migrate from SQLite not implemented")
}

// Close closes the database.
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}