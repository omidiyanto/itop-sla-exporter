package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"gopkg.in/yaml.v2"

	"itop-sla-exporter/internal/itop"
	"itop-sla-exporter/internal/utils"
)

type Config struct {
	WorkHours struct {
		Start string `yaml:"start"`
		End   string `yaml:"end"`
	} `yaml:"work_hours"`
	Holidays     []string `yaml:"holidays"`
	SLADeadlines map[string]map[string]struct {
		Response string `yaml:"response"`
		Resolve  string `yaml:"resolve"`
	} `yaml:"sla_deadlines"`
}

var config Config

func loadConfig(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	decoder := yaml.NewDecoder(f)
	return decoder.Decode(&config)
}

func main() {
	// Load config
	err := loadConfig("config/business_hours.yaml")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Register Prometheus metrics
	prometheus.MustRegister(ticketCount)
	prometheus.MustRegister(slaCompliance)

	// Periodic fetcher
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			updateMetrics()
			<-ticker.C
		}
	}()

	http.Handle("/metrics", promhttp.Handler())
	fmt.Println("Exporter running on :9100/metrics")
	log.Fatal(http.ListenAndServe(":9100", nil))
}

func priorityLabel(id string) string {
	switch id {
	case "1":
		return "Critical"
	case "2":
		return "High"
	case "3":
		return "Medium"
	case "4":
		return "Low"
	default:
		return id
	}
}

func urgencyLabel(id string) string {
	switch id {
	case "1":
		return "Critical"
	case "2":
		return "High"
	case "3":
		return "Medium"
	case "4":
		return "Low"
	default:
		return id
	}
}

func updateMetrics() {
	tickets, err := itop.FetchTickets()
	if err != nil {
		log.Printf("Error fetching tickets: %v", err)
		return
	}
	// Reset metrics
	ticketCount.Reset()
	slaCompliance.Reset()
	// Prepare holidays map
	holidays := make(map[string]struct{})
	for _, h := range config.Holidays {
		holidays[h] = struct{}{}
	}
	workStart := config.WorkHours.Start
	workEnd := config.WorkHours.End

	for _, t := range tickets {
		prio := priorityLabel(t.Priority)
		urg := urgencyLabel(t.Urgency)
		ticketCount.WithLabelValues(
			t.Status, t.Class, t.Service, t.ServiceSubcategory, t.Team, t.Agent, prio, urg,
		).Inc()

		// SLA deadline from config
		prioKey := strings.ToLower(prio)
		var responseDeadline, resolveDeadline time.Duration
		if d, ok := config.SLADeadlines[t.Class][prioKey]; ok {
			responseDeadline, _ = parseDuration(d.Response)
			resolveDeadline, _ = parseDuration(d.Resolve)
		}

		// RAW calculation
		ttrRaw := t.ResolutionDate.Sub(t.StartDate)
		ttoRaw := t.AssignmentDate.Sub(t.StartDate)
		complyResponseRaw := 0.0
		complyResolveRaw := 0.0
		if responseDeadline > 0 && ttoRaw > 0 {
			if ttoRaw <= responseDeadline {
				complyResponseRaw = 1.0
			}
		}
		if resolveDeadline > 0 && ttrRaw > 0 {
			if ttrRaw <= resolveDeadline {
				complyResolveRaw = 1.0
			}
		}
		slaCompliance.WithLabelValues(t.Class, prio, urg, "raw", "response", "comply").Add(complyResponseRaw)
		slaCompliance.WithLabelValues(t.Class, prio, urg, "raw", "response", "violate").Add(1.0 - complyResponseRaw)
		slaCompliance.WithLabelValues(t.Class, prio, urg, "raw", "resolve", "comply").Add(complyResolveRaw)
		slaCompliance.WithLabelValues(t.Class, prio, urg, "raw", "resolve", "violate").Add(1.0 - complyResolveRaw)

		// BUSINESS-HOUR calculation
		ttrBH := utils.CalculateBusinessHourDuration(t.StartDate, t.ResolutionDate, workStart, workEnd, holidays)
		ttoBH := utils.CalculateBusinessHourDuration(t.StartDate, t.AssignmentDate, workStart, workEnd, holidays)
		complyResponseBH := 0.0
		complyResolveBH := 0.0
		if responseDeadline > 0 && ttoBH > 0 {
			if ttoBH <= responseDeadline {
				complyResponseBH = 1.0
			}
		}
		if resolveDeadline > 0 && ttrBH > 0 {
			if ttrBH <= resolveDeadline {
				complyResolveBH = 1.0
			}
		}
		slaCompliance.WithLabelValues(t.Class, prio, urg, "business-hour", "response", "comply").Add(complyResponseBH)
		slaCompliance.WithLabelValues(t.Class, prio, urg, "business-hour", "response", "violate").Add(1.0 - complyResponseBH)
		slaCompliance.WithLabelValues(t.Class, prio, urg, "business-hour", "resolve", "comply").Add(complyResolveBH)
		slaCompliance.WithLabelValues(t.Class, prio, urg, "business-hour", "resolve", "violate").Add(1.0 - complyResolveBH)
	}
}

func parseDuration(s string) (time.Duration, error) {
	// Accepts "4h", "30m", etc.
	return time.ParseDuration(s)
}

// Prometheus metrics
var (
	ticketCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "itop_ticket_count",
			Help: "Number of tickets by status, class, service, service_subcategory, team, agent, priority, urgency.",
		},
		[]string{"status", "class", "service", "service_subcategory", "team", "agent", "priority", "urgency"},
	)

	slaCompliance = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "itop_ticket_sla_compliance",
			Help: "SLA compliance by class, priority, urgency, sla_type, sla_metric, status.",
		},
		[]string{"class", "priority", "urgency", "sla_type", "sla_metric", "status"},
	)
)
