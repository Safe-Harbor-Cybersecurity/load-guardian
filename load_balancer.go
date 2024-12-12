package main

import (
    "fmt"
    "log"
    "net/http"
    "net/http/httputil"
    "net/url"
    "sync"
    "time"
    "encoding/json"
    "os"
)

// Config represents the load balancer configuration
type Config struct {
    Backends        []string `json:"backends"`
    RateLimit      int      `json:"rate_limit"`      // Requests per minute per IP
    BurstLimit     int      `json:"burst_limit"`     // Maximum burst size
    BlockThreshold int      `json:"block_threshold"` // Number of violations before blocking
    BlockDuration  int      `json:"block_duration"`  // Block duration in minutes
}

// Backend represents a backend server
type Backend struct {
    URL          *url.URL
    Alive        bool
    ReverseProxy *httputil.ReverseProxy
    LastChecked  time.Time
}

// RequestTracker tracks client request rates
type RequestTracker struct {
    Count     int
    LastReset time.Time
    Violations int
    BlockedUntil time.Time
}

type LoadBalancer struct {
    backends     []*Backend
    nextBackend  int
    mux         sync.RWMutex
    config      Config
    trackers    map[string]*RequestTracker
    trackerMux  sync.RWMutex
}

func NewLoadBalancer(configPath string) (*LoadBalancer, error) {
    // Read configuration
    file, err := os.ReadFile(configPath)
    if err != nil {
        return nil, fmt.Errorf("error reading config: %v", err)
    }

    var config Config
    if err := json.Unmarshal(file, &config); err != nil {
        return nil, fmt.Errorf("error parsing config: %v", err)
    }

    lb := &LoadBalancer{
        config:   config,
        trackers: make(map[string]*RequestTracker),
    }

    // Initialize backends
    for _, backend := range config.Backends {
        url, err := url.Parse(backend)
        if err != nil {
            return nil, fmt.Errorf("error parsing backend URL %s: %v", backend, err)
        }

        proxy := httputil.NewSingleHostReverseProxy(url)
        proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
            log.Printf("Proxy error: %v", err)
            lb.markBackendDown(backend)
            http.Error(w, "Service temporarily unavailable", http.StatusServiceUnavailable)
        }

        lb.backends = append(lb.backends, &Backend{
            URL:          url,
            Alive:        true,
            ReverseProxy: proxy,
        })
    }

    return lb, nil
}

func (lb *LoadBalancer) markBackendDown(backendURL string) {
    lb.mux.Lock()
    defer lb.mux.Unlock()

    for _, backend := range lb.backends {
        if backend.URL.String() == backendURL {
            backend.Alive = false
            backend.LastChecked = time.Now()
        }
    }
}

func (lb *LoadBalancer) isIPBlocked(ip string) bool {
    lb.trackerMux.RLock()
    defer lb.trackerMux.RUnlock()

    if tracker, exists := lb.trackers[ip]; exists {
        return time.Now().Before(tracker.BlockedUntil)
    }
    return false
}

func (lb *LoadBalancer) trackRequest(ip string) bool {
    lb.trackerMux.Lock()
    defer lb.trackerMux.Unlock()

    now := time.Now()
    tracker, exists := lb.trackers[ip]
    
    if !exists {
        lb.trackers[ip] = &RequestTracker{
            Count:     1,
            LastReset: now,
        }
        return true
    }

    // Reset counter if minute has passed
    if now.Sub(tracker.LastReset) > time.Minute {
        tracker.Count = 1
        tracker.LastReset = now
        return true
    }

    // Check rate limit
    if tracker.Count >= lb.config.RateLimit {
        tracker.Violations++
        
        // Block IP if violations exceed threshold
        if tracker.Violations >= lb.config.BlockThreshold {
            tracker.BlockedUntil = now.Add(time.Duration(lb.config.BlockDuration) * time.Minute)
            log.Printf("Blocking IP %s for %d minutes due to rate limit violations", 
                      ip, lb.config.BlockDuration)
            return false
        }
        return false
    }

    tracker.Count++
    return true
}

func (lb *LoadBalancer) getNextBackend() *Backend {
    lb.mux.Lock()
    defer lb.mux.Unlock()

    // Simple round-robin selection of healthy backends
    for i := 0; i < len(lb.backends); i++ {
        lb.nextBackend = (lb.nextBackend + 1) % len(lb.backends)
        if lb.backends[lb.nextBackend].Alive {
            return lb.backends[lb.nextBackend]
        }
    }
    return nil
}

func (lb *LoadBalancer) healthCheck() {
    for {
        for _, backend := range lb.backends {
            if !backend.Alive && time.Since(backend.LastChecked) > time.Minute {
                resp, err := http.Get(backend.URL.String() + "/health")
                if err == nil && resp.StatusCode == http.StatusOK {
                    lb.mux.Lock()
                    backend.Alive = true
                    lb.mux.Unlock()
                    log.Printf("Backend %s is back online", backend.URL)
                }
                backend.LastChecked = time.Now()
            }
        }
        time.Sleep(time.Second * 30)
    }
}

func (lb *LoadBalancer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    clientIP := r.RemoteAddr

    // Check if IP is blocked
    if lb.isIPBlocked(clientIP) {
        http.Error(w, "Too many requests", http.StatusTooManyRequests)
        return
    }

    // Track and check rate limit
    if !lb.trackRequest(clientIP) {
        http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
        return
    }

    // Get next available backend
    backend := lb.getNextBackend()
    if backend == nil {
        http.Error(w, "No backends available", http.StatusServiceUnavailable)
        return
    }

    // Forward the request
    backend.ReverseProxy.ServeHTTP(w, r)
}

func main() {
    lb, err := NewLoadBalancer("config.json")
    if err != nil {
        log.Fatal(err)
    }

    // Start health checker
    go lb.healthCheck()

    // Start the server
    server := &http.Server{
        Addr:    ":8080",
        Handler: lb,
    }

    log.Printf("Load balancer started on :8080")
    if err := server.ListenAndServe(); err != nil {
        log.Fatal(err)
    }
}
