// SPDX-License-Identifier: MIT

// Package dashboard provides a web-based dashboard for xg2g.
package dashboard

import (
	"encoding/json"
	"html/template"
	"net/http"
	"runtime"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/rs/zerolog"
)

// Dashboard holds the dashboard service
type Dashboard struct {
	config config.AppConfig
	logger zerolog.Logger
	stats  *ServiceStats
}

// ServiceStats tracks runtime statistics
type ServiceStats struct {
	StartTime       time.Time `json:"start_time"`
	LastRefresh     time.Time `json:"last_refresh"`
	RefreshCount    int64     `json:"refresh_count"`
	ErrorCount      int64     `json:"error_count"`
	ChannelsActive  int       `json:"channels_active"`
	BouquetsActive  int       `json:"bouquets_active"`
	EPGProgrammes   int       `json:"epg_programmes"`
	MemoryUsageMB   float64   `json:"memory_usage_mb"`
	GoroutineCount  int       `json:"goroutine_count"`
	UptimeSeconds   int64     `json:"uptime_seconds"`
	RequestsServed  int64     `json:"requests_served"`
	AvgResponseTime float64   `json:"avg_response_time_ms"`
}

// New creates a new dashboard instance
func New(config config.AppConfig, logger zerolog.Logger) *Dashboard {
	return &Dashboard{
		config: config,
		logger: logger,
		stats: &ServiceStats{
			StartTime: time.Now(),
		},
	}
}

// UpdateStats updates runtime statistics
func (d *Dashboard) UpdateStats() {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	d.stats.MemoryUsageMB = float64(m.Alloc) / 1024 / 1024
	d.stats.GoroutineCount = runtime.NumGoroutine()
	d.stats.UptimeSeconds = int64(time.Since(d.stats.StartTime).Seconds())
}

// RecordRefresh records a refresh operation
func (d *Dashboard) RecordRefresh(success bool, channels, bouquets, programmes int) {
	d.stats.LastRefresh = time.Now()
	d.stats.RefreshCount++

	if !success {
		d.stats.ErrorCount++
	} else {
		d.stats.ChannelsActive = channels
		d.stats.BouquetsActive = bouquets
		d.stats.EPGProgrammes = programmes
	}
}

// RecordRequest records an HTTP request
func (d *Dashboard) RecordRequest(duration time.Duration) {
	d.stats.RequestsServed++

	// Simple moving average for response time
	if d.stats.RequestsServed == 1 {
		d.stats.AvgResponseTime = float64(duration.Nanoseconds()) / 1e6 // Convert to milliseconds
	} else {
		// Exponential moving average with alpha = 0.1
		alpha := 0.1
		current := float64(duration.Nanoseconds()) / 1e6
		d.stats.AvgResponseTime = alpha*current + (1-alpha)*d.stats.AvgResponseTime
	}
}

// HandleDashboard serves the HTML dashboard
func (d *Dashboard) HandleDashboard(w http.ResponseWriter, _ *http.Request) {
	d.UpdateStats()

	dashboardHTML := `
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>xg2g Dashboard</title>
    <style>
        * { margin: 0; padding: 0; box-sizing: border-box; }
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Oxygen, Ubuntu, Cantarell, sans-serif;
            background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
            min-height: 100vh;
            padding: 20px;
        }
        .container {
            max-width: 1200px;
            margin: 0 auto;
            background: rgba(255, 255, 255, 0.95);
            border-radius: 15px;
            backdrop-filter: blur(10px);
            box-shadow: 0 20px 40px rgba(0, 0, 0, 0.1);
            overflow: hidden;
        }
        .header {
            background: linear-gradient(90deg, #4f46e5, #7c3aed);
            color: white;
            padding: 30px;
            text-align: center;
        }
        .header h1 {
            font-size: 2.5em;
            margin-bottom: 10px;
            text-shadow: 0 2px 4px rgba(0, 0, 0, 0.3);
        }
        .header .version {
            opacity: 0.9;
            font-size: 1.1em;
        }
        .stats-grid {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
            gap: 25px;
            padding: 30px;
        }
        .stat-card {
            background: white;
            border-radius: 12px;
            padding: 25px;
            box-shadow: 0 4px 15px rgba(0, 0, 0, 0.1);
            border-left: 4px solid;
            transition: transform 0.2s ease, box-shadow 0.2s ease;
        }
        .stat-card:hover {
            transform: translateY(-2px);
            box-shadow: 0 8px 25px rgba(0, 0, 0, 0.15);
        }
        .stat-card.primary { border-left-color: #3b82f6; }
        .stat-card.success { border-left-color: #10b981; }
        .stat-card.warning { border-left-color: #f59e0b; }
        .stat-card.danger { border-left-color: #ef4444; }
        .stat-card.info { border-left-color: #8b5cf6; }

        .stat-value {
            font-size: 2.2em;
            font-weight: bold;
            margin-bottom: 8px;
        }
        .stat-card.primary .stat-value { color: #3b82f6; }
        .stat-card.success .stat-value { color: #10b981; }
        .stat-card.warning .stat-value { color: #f59e0b; }
        .stat-card.danger .stat-value { color: #ef4444; }
        .stat-card.info .stat-value { color: #8b5cf6; }

        .stat-label {
            color: #6b7280;
            font-weight: 500;
            text-transform: uppercase;
            font-size: 0.85em;
            letter-spacing: 0.5px;
        }
        .status-indicator {
            display: inline-block;
            width: 12px;
            height: 12px;
            border-radius: 50%;
            margin-right: 8px;
            animation: pulse 2s infinite;
        }
        .status-online { background-color: #10b981; }
        .status-offline { background-color: #ef4444; }

        @keyframes pulse {
            0% { box-shadow: 0 0 0 0 rgba(16, 185, 129, 0.7); }
            70% { box-shadow: 0 0 0 10px rgba(16, 185, 129, 0); }
            100% { box-shadow: 0 0 0 0 rgba(16, 185, 129, 0); }
        }

        .refresh-btn {
            background: linear-gradient(45deg, #4f46e5, #7c3aed);
            color: white;
            border: none;
            padding: 12px 24px;
            border-radius: 8px;
            cursor: pointer;
            font-weight: 600;
            margin: 20px auto;
            display: block;
            transition: all 0.2s ease;
        }
        .refresh-btn:hover {
            transform: scale(1.05);
            box-shadow: 0 4px 15px rgba(79, 70, 229, 0.4);
        }

        .config-section {
            margin: 20px 30px;
            padding: 25px;
            background: #f8fafc;
            border-radius: 12px;
            border: 1px solid #e2e8f0;
        }
        .config-title {
            font-size: 1.2em;
            font-weight: 600;
            margin-bottom: 15px;
            color: #1e293b;
        }
        .config-grid {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(250px, 1fr));
            gap: 15px;
        }
        .config-item {
            display: flex;
            justify-content: space-between;
            padding: 8px 0;
            border-bottom: 1px solid #e2e8f0;
        }
        .config-key { font-weight: 500; color: #475569; }
        .config-value {
            color: #1e293b;
            font-family: 'Monaco', 'Menlo', 'Ubuntu Mono', monospace;
            font-size: 0.9em;
        }

        @media (max-width: 768px) {
            .stats-grid {
                grid-template-columns: 1fr;
                padding: 20px;
            }
            .header h1 { font-size: 2em; }
            .config-section { margin: 20px; }
        }
    </style>
    <script>
        function refreshData() {
            location.reload();
        }

        function formatUptime(seconds) {
            const days = Math.floor(seconds / 86400);
            const hours = Math.floor((seconds % 86400) / 3600);
            const minutes = Math.floor((seconds % 3600) / 60);

            if (days > 0) return days + "d " + hours + "h " + minutes + "m";
            if (hours > 0) return hours + "h " + minutes + "m";
            return minutes + "m " + (seconds % 60) + "s";
        }

        // Auto-refresh every 30 seconds
        setTimeout(refreshData, 30000);
    </script>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>🎬 xg2g Dashboard</h1>
            <div class="version">
                <span class="status-indicator status-online"></span>
                Version {{.Config.Version}} • Running since {{.Stats.StartTime.Format "2006-01-02 15:04:05"}}
            </div>
        </div>

        <div class="stats-grid">
            <div class="stat-card success">
                <div class="stat-value">{{.Stats.ChannelsActive}}</div>
                <div class="stat-label">Active Channels</div>
            </div>

            <div class="stat-card info">
                <div class="stat-value">{{.Stats.BouquetsActive}}</div>
                <div class="stat-label">Bouquets</div>
            </div>

            <div class="stat-card primary">
                <div class="stat-value">{{.Stats.EPGProgrammes}}</div>
                <div class="stat-label">EPG Programmes</div>
            </div>

            <div class="stat-card warning">
                <div class="stat-value">{{printf "%.1f" .Stats.MemoryUsageMB}} MB</div>
                <div class="stat-label">Memory Usage</div>
            </div>

            <div class="stat-card info">
                <div class="stat-value">{{.Stats.GoroutineCount}}</div>
                <div class="stat-label">Goroutines</div>
            </div>

            <div class="stat-card success">
                <div class="stat-value">{{.Stats.RefreshCount}}</div>
                <div class="stat-label">Total Refreshes</div>
            </div>

            <div class="stat-card {{if gt .Stats.ErrorCount 0}}danger{{else}}success{{end}}">
                <div class="stat-value">{{.Stats.ErrorCount}}</div>
                <div class="stat-label">Errors</div>
            </div>

            <div class="stat-card primary">
                <div class="stat-value">{{.Stats.RequestsServed}}</div>
                <div class="stat-label">Requests Served</div>
            </div>

            <div class="stat-card info">
                <div class="stat-value">{{printf "%.1f" .Stats.AvgResponseTime}}ms</div>
                <div class="stat-label">Avg Response Time</div>
            </div>
        </div>

        <button class="refresh-btn" onclick="refreshData()">🔄 Refresh Now</button>

        <div class="config-section">
            <div class="config-title">📋 Configuration</div>
            <div class="config-grid">
                <div class="config-item">
                    <span class="config-key">OpenWebIF URL:</span>
                    <span class="config-value">{{.Config.OWIBase}}</span>
                </div>
                <div class="config-item">
                    <span class="config-key">Bouquet:</span>
                    <span class="config-value">{{.Config.Bouquet}}</span>
                </div>
                <div class="config-item">
                    <span class="config-key">Stream Port:</span>
                    <span class="config-value">{{.Config.StreamPort}}</span>
                </div>
                <div class="config-item">
                    <span class="config-key">EPG Enabled:</span>
                    <span class="config-value">{{if .Config.EPGEnabled}}✅ Yes{{else}}❌ No{{end}}</span>
                </div>
                <div class="config-item">
                    <span class="config-key">EPG Days:</span>
                    <span class="config-value">{{.Config.EPGDays}}</span>
                </div>
                <div class="config-item">
                    <span class="config-key">Data Directory:</span>
                    <span class="config-value">{{.Config.DataDir}}</span>
                </div>
            </div>
        </div>
    </div>
</body>
</html>
`

	tmpl, err := template.New("dashboard").Parse(dashboardHTML)
	if err != nil {
		http.Error(w, "Template error", http.StatusInternalServerError)
		d.logger.Error().Err(err).Msg("dashboard template parse failed")
		return
	}

	data := struct {
		Config config.AppConfig
		Stats  *ServiceStats
	}{
		Config: d.config,
		Stats:  d.stats,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.Execute(w, data); err != nil {
		d.logger.Error().Err(err).Msg("dashboard template execution failed")
	}
}

// HandleAPIStats serves JSON stats for API consumption
func (d *Dashboard) HandleAPIStats(w http.ResponseWriter, _ *http.Request) {
	d.UpdateStats()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(d.stats); err != nil {
		http.Error(w, "JSON encoding error", http.StatusInternalServerError)
		d.logger.Error().Err(err).Msg("stats JSON encoding failed")
	}
}

// Middleware wraps HTTP handlers to record request statistics
func (d *Dashboard) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Call the next handler
		next.ServeHTTP(w, r)

		// Record the request
		d.RecordRequest(time.Since(start))
	})
}
