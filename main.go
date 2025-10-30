package main

import (
	"crypto/rand"
	"embed"
	"encoding/hex"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

//go:embed templates/*
var templateFiles embed.FS

func generateID() string {
	bytes := make([]byte, 8)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

type Paste struct {
	ID    string
	Title string
	Body  []byte
	TTL   string
}

var TTLHours = map[string]int{
	"1h":  1,
	"3h":  3,
	"6h":  6,
	"12h": 12,
	"24h": 24,
	"3d":  72,
	"7d":  168,
}

func (p *Paste) save() error {
	// Create subdirectory using first 2 chars of ID (256 buckets)
	subdir := fmt.Sprintf("pastes/%s", p.ID[:2])
	os.MkdirAll(subdir, 0755)
	
	// Save content as plain text 
	content := p.Title + "\n" + string(p.Body)
	filename := fmt.Sprintf("%s/%s_%s.txt", subdir, p.ID, p.TTL)
	
	file, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	
	_, err = file.Write([]byte(content))
	if err != nil {
		return err
	}
	
	// Force sync to disk
	err = file.Sync()
	if err != nil {
		return err
	}
	
	return nil
}

var cleanupOffset int

func cleanupExpired() {
	now := time.Now().Unix()
	
	// Process 16 subdirs per cycle (full scan in ~8 hours)
	start := cleanupOffset
	end := cleanupOffset + 16
	
	for i := start; i < end; i++ {
		subdir := fmt.Sprintf("pastes/%02x", i)
		
		entries, err := os.ReadDir(subdir)
		if err != nil {
			continue
		}
		
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".txt") {
				continue
			}
			
			// Parse filename: id_ttl.txt
			name := strings.TrimSuffix(entry.Name(), ".txt")
			parts := strings.Split(name, "_")
			if len(parts) != 2 {
				continue
			}
			
			// Get file modification time
			filePath := filepath.Join(subdir, entry.Name())
			info, err := os.Stat(filePath)
			if err != nil {
				continue
			}
			
			createdAt := info.ModTime().Unix()
			
			// Calculate expiration using TTL
			ttlHours, exists := TTLHours[parts[1]]
			if !exists {
				continue
			}
			
			expiresAt := createdAt + int64(ttlHours*3600)
			if now > expiresAt {
				os.Remove(filePath)
			}
		}
	}
	
	cleanupOffset = (cleanupOffset + 16) % 256
}

func loadPaste(id string) (*Paste, error) {
	// Find file by scanning subdirectory for matching ID
	subdir := fmt.Sprintf("pastes/%s", id[:2])
	files, err := filepath.Glob(subdir + "/" + id + "_*.txt")
	if err != nil || len(files) == 0 {
		return nil, fmt.Errorf("paste not found")
	}
	
	filename := files[0]
	
	// Use file mtime as creation time
	info, err := os.Stat(filename)
	if err != nil {
		return nil, err
	}
	createdAt := info.ModTime().Unix()
	
	// Parse TTL from filename
	basename := filepath.Base(filename)
	parts := strings.Split(strings.TrimSuffix(basename, ".txt"), "_")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid paste file format")
	}
	
	ttl := parts[1]
	ttlHours, exists := TTLHours[ttl]
	if !exists {
		return nil, fmt.Errorf("invalid TTL")
	}
	
	expiresAt := createdAt + int64(ttlHours*3600)
	
	// Check if expired
	if time.Now().Unix() > expiresAt {
		os.Remove(filename) // Clean up expired paste
		return nil, fmt.Errorf("paste expired")
	}
	
	content, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	
	lines := strings.SplitN(string(content), "\n", 2)
	if len(lines) < 2 {
		return nil, fmt.Errorf("invalid paste content")
	}
	
	return &Paste{
		ID:    id,
		Title: lines[0],
		Body:  []byte(lines[1]),
		TTL:   ttl,
	}, nil
}



func saveHandler(w http.ResponseWriter, r *http.Request) {
	// Only allow POST requests
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	title := r.FormValue("title")
	body := r.FormValue("body")
	ttl := r.FormValue("ttl")
	
	// Basic size limits
	if len(title) > 200 {
		http.Error(w, "Title too long (max 200 chars)", http.StatusBadRequest)
		return
	}
	if len(body) > 1024*1024 { // 1MB limit
		http.Error(w, "Content too large (max 1MB)", http.StatusBadRequest)
		return
	}
	if title == "" || body == "" {
		http.Error(w, "Title and content required", http.StatusBadRequest)
		return
	}
	
	// Default to 6h if no TTL specified
	if ttl == "" {
		ttl = "6h"
	}
	
	// Validate TTL
	_, exists := TTLHours[ttl]
	if !exists {
		http.Error(w, "Invalid TTL", http.StatusBadRequest)
		return
	}
	
	id := generateID()
	
	p := &Paste{
		ID:    id,
		Title: title,
		Body:  []byte(body),
		TTL:   ttl,
	}
	
	err := p.save()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/"+id, http.StatusFound)
}

var templates = template.Must(template.ParseFS(templateFiles, "templates/*.html"))

func renderTemplate(w http.ResponseWriter, tmpl string, p *Paste) {
	err := templates.ExecuteTemplate(w, tmpl+".html", p)
	if err != nil {
		log.Printf("Template error: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

func isValidID(id string) bool {
	// Only allow hex characters, 16 chars long (8 bytes * 2)
	if len(id) != 16 {
		return false
	}
	for _, c := range id {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

func mainHandler(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	
	switch path {
	case "/":
		renderTemplate(w, "index", nil)
		return
	case "/about":
		renderTemplate(w, "about", nil)
		return
	case "/legal":
		renderTemplate(w, "legal", nil)
		return
	}
	
	id := strings.TrimPrefix(path, "/")
	
	// Validate ID format
	if !isValidID(id) {
		http.NotFound(w, r)
		return
	}
	
	p, err := loadPaste(id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	renderTemplate(w, "view", p)
}

func main() {
	// Cleanup job runs every 30min
	go func() {
		for {
			time.Sleep(30 * time.Minute)
			cleanupExpired()
		}
	}()

	http.HandleFunc("/", mainHandler)
	http.HandleFunc("/save", saveHandler)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Starting server on port %s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
