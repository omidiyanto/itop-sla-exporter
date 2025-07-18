package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
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
	// Holidays removed: now loaded from holidays.txt
	SLADeadlines map[string]map[string]struct {
		Response string `yaml:"response"`
		Resolve  string `yaml:"resolve"`
	} `yaml:"sla_deadlines"`
}

// Fungsi summary metrics updater

func updateSummaryMetrics(tickets []itop.Ticket) {
	ticketCount.Reset()
	slaCompliance.Reset()

	// Load holidays from file (sync with iTop)
	holidaysList, _ := itop.LoadHolidaysFromFile("holidays.txt")
	holidays := make(map[string]struct{})
	for _, h := range holidaysList {
		holidays[h] = struct{}{}
	}
	workStart := config.WorkHours.Start
	workEnd := config.WorkHours.End

	type agg struct {
		sumResponse float64
		sumResolve  float64
		count       float64
	}
	avgRespMap := make(map[string]*agg)
	avgResMap := make(map[string]*agg)
	monthlyMap := make(map[string]float64)

	for _, t := range tickets {
		prio := priorityLabel(t.Priority)
		urg := urgencyLabel(t.Urgency)
		ticketCount.WithLabelValues(
			t.Status, t.Class, t.Service, t.ServiceSubcategory, t.Team, t.Agent, prio, urg,
		).Inc()

		// Monthly ticket count
		month := t.StartDate.Format("2006-01")
		monthlyKey := strings.Join([]string{month, t.Class, t.Status, t.Agent, t.Team}, "|")
		monthlyMap[monthlyKey]++

		// Histogram & average
		ttrRaw := t.ResolutionDate.Sub(t.StartDate).Seconds()
		ttoRaw := t.AssignmentDate.Sub(t.StartDate).Seconds()
		key := strings.Join([]string{t.Class, prio, urg, t.Agent, t.Team}, "|")
		if ttoRaw > 0 {
			if avgRespMap[key] == nil {
				avgRespMap[key] = &agg{}
			}
			avgRespMap[key].sumResponse += ttoRaw
			avgRespMap[key].count++
		}
		if ttrRaw > 0 {
			if avgResMap[key] == nil {
				avgResMap[key] = &agg{}
			}
			avgResMap[key].sumResolve += ttrRaw
			avgResMap[key].count++
		}

		// Ticket age (for open/assigned tickets)

		// SLA deadline from iTop (not config, now cached)
		slt, err := itop.GetSLTDeadlineCached(t.Class, t.Priority, t.Service)
		var responseDeadline, resolveDeadline time.Duration
		if err == nil {
			responseDeadline = slt.TTO
			resolveDeadline = slt.TTR
			// log.Printf("SLT fetched for class=%s, priority=%s, service=%s: TTO=%v, TTR=%v", t.Class, t.Priority, t.Service, slt.TTO, slt.TTR)
		} else {
			// log.Printf("SLT fetch error for class=%s, priority=%s, service=%s: %v", t.Class, t.Priority, t.Service, err)
		}

		// RAW calculation
		complyResponseRaw := 0.0
		complyResolveRaw := 0.0
		if responseDeadline > 0 && ttoRaw > 0 {
			if ttoRaw <= responseDeadline.Seconds() {
				complyResponseRaw = 1.0
			}
		}
		if resolveDeadline > 0 && ttrRaw > 0 {
			if ttrRaw <= resolveDeadline.Seconds() {
				complyResolveRaw = 1.0
			}
		}
		slaCompliance.WithLabelValues(t.Class, prio, urg, "raw", "response", "comply").Add(complyResponseRaw)
		slaCompliance.WithLabelValues(t.Class, prio, urg, "raw", "response", "violate").Add(1.0 - complyResponseRaw)
		slaCompliance.WithLabelValues(t.Class, prio, urg, "raw", "resolve", "comply").Add(complyResolveRaw)
		slaCompliance.WithLabelValues(t.Class, prio, urg, "raw", "resolve", "violate").Add(1.0 - complyResolveRaw)

		// BUSINESS-HOUR calculation
		ttrBH := utils.CalculateBusinessHourDuration(t.StartDate, t.ResolutionDate, workStart, workEnd, holidays).Seconds()
		ttoBH := utils.CalculateBusinessHourDuration(t.StartDate, t.AssignmentDate, workStart, workEnd, holidays).Seconds()
		complyResponseBH := 0.0
		complyResolveBH := 0.0
		if responseDeadline > 0 && ttoBH > 0 {
			if ttoBH <= responseDeadline.Seconds() {
				complyResponseBH = 1.0
			}
		}
		if resolveDeadline > 0 && ttrBH > 0 {
			if ttrBH <= resolveDeadline.Seconds() {
				complyResolveBH = 1.0
			}
		}
		slaCompliance.WithLabelValues(t.Class, prio, urg, "business-hour", "response", "comply").Add(complyResponseBH)
		slaCompliance.WithLabelValues(t.Class, prio, urg, "business-hour", "response", "violate").Add(1.0 - complyResponseBH)
		slaCompliance.WithLabelValues(t.Class, prio, urg, "business-hour", "resolve", "comply").Add(complyResolveBH)
		slaCompliance.WithLabelValues(t.Class, prio, urg, "business-hour", "resolve", "violate").Add(1.0 - complyResolveBH)
	}

	// Set average metrics
}

// Fungsi set metric detail per ticket
func setTicketDetailMetric(t itop.Ticket) {
	prio := priorityLabel(t.Priority)
	urg := urgencyLabel(t.Urgency)
	var ttrRaw, ttoRaw, ttrBH, ttoBH float64
	var startDateStr, assignmentDateStr, resolutionDateStr string
	workStart := config.WorkHours.Start
	workEnd := config.WorkHours.End
	holidaysList, _ := itop.LoadHolidaysFromFile("holidays.txt")
	holidays := make(map[string]struct{})
	for _, h := range holidaysList {
		holidays[h] = struct{}{}
	}
	if t.StartDate.IsZero() {
		startDateStr = ""
	} else {
		startDateStr = fmt.Sprintf("%d", t.StartDate.Unix())
	}
	if t.AssignmentDate.IsZero() {
		assignmentDateStr = ""
	} else {
		assignmentDateStr = fmt.Sprintf("%d", t.AssignmentDate.Unix())
	}
	if t.ResolutionDate.IsZero() {
		resolutionDateStr = ""
	} else {
		resolutionDateStr = fmt.Sprintf("%d", t.ResolutionDate.Unix())
	}
	if t.StartDate.IsZero() || t.AssignmentDate.IsZero() {
		ttoRaw = 0
		ttoBH = 0
	} else {
		ttoRaw = t.AssignmentDate.Sub(t.StartDate).Seconds()
		ttoBH = utils.CalculateBusinessHourDuration(t.StartDate, t.AssignmentDate, workStart, workEnd, holidays).Seconds()
	}
	if t.StartDate.IsZero() || t.ResolutionDate.IsZero() {
		ttrRaw = 0
		ttrBH = 0
	} else {
		ttrRaw = t.ResolutionDate.Sub(t.StartDate).Seconds()
		ttrBH = utils.CalculateBusinessHourDuration(t.StartDate, t.ResolutionDate, workStart, workEnd, holidays).Seconds()
	}

	// Ambil SLT deadline dari cache
	slt, err := itop.GetSLTDeadlineCached(t.Class, t.Priority, t.Service)
	var responseDeadline, resolveDeadline time.Duration
	if err == nil {
		responseDeadline = slt.TTO
		resolveDeadline = slt.TTR
	}

	// Compliance logic per metric
	slaComplyRawResp := "violate"
	slaComplyRawRes := "violate"
	slaComplyBHResp := "violate"
	slaComplyBHRes := "violate"
	if responseDeadline > 0 && ttoRaw > 0 && ttoRaw <= responseDeadline.Seconds() {
		slaComplyRawResp = "comply"
	}
	if resolveDeadline > 0 && ttrRaw > 0 && ttrRaw <= resolveDeadline.Seconds() {
		slaComplyRawRes = "comply"
	}
	if responseDeadline > 0 && ttoBH > 0 && ttoBH <= responseDeadline.Seconds() {
		slaComplyBHResp = "comply"
	}
	if resolveDeadline > 0 && ttrBH > 0 && ttrBH <= resolveDeadline.Seconds() {
		slaComplyBHRes = "comply"
	}

	// Emit business-hour metric (response)
	ticketDetailInfo.WithLabelValues(
		t.ID,
		t.Ref,
		t.Class,
		t.Title,
		t.Status,
		prio,
		urg,
		t.Impact,
		t.Service,
		t.ServiceSubcategory,
		t.Agent,
		t.Team,
		startDateStr,
		assignmentDateStr,
		resolutionDateStr,
		fmt.Sprintf("%.0f", ttoBH),
		fmt.Sprintf("%.0f", ttrBH),
		"business-hour",
		"response",
		slaComplyBHResp,
	).Set(1)

	// Emit business-hour metric (resolve)
	ticketDetailInfo.WithLabelValues(
		t.ID,
		t.Ref,
		t.Class,
		t.Title,
		t.Status,
		prio,
		urg,
		t.Impact,
		t.Service,
		t.ServiceSubcategory,
		t.Agent,
		t.Team,
		startDateStr,
		assignmentDateStr,
		resolutionDateStr,
		fmt.Sprintf("%.0f", ttoBH),
		fmt.Sprintf("%.0f", ttrBH),
		"business-hour",
		"resolve",
		slaComplyBHRes,
	).Set(1)

	// Emit raw metric (response)
	ticketDetailInfo.WithLabelValues(
		t.ID,
		t.Ref,
		t.Class,
		t.Title,
		t.Status,
		prio,
		urg,
		t.Impact,
		t.Service,
		t.ServiceSubcategory,
		t.Agent,
		t.Team,
		startDateStr,
		assignmentDateStr,
		resolutionDateStr,
		fmt.Sprintf("%.0f", ttoRaw),
		fmt.Sprintf("%.0f", ttrRaw),
		"raw",
		"response",
		slaComplyRawResp,
	).Set(1)

	// Emit raw metric (resolve)
	ticketDetailInfo.WithLabelValues(
		t.ID,
		t.Ref,
		t.Class,
		t.Title,
		t.Status,
		prio,
		urg,
		t.Impact,
		t.Service,
		t.ServiceSubcategory,
		t.Agent,
		t.Team,
		startDateStr,
		assignmentDateStr,
		resolutionDateStr,
		fmt.Sprintf("%.0f", ttoRaw),
		fmt.Sprintf("%.0f", ttrRaw),
		"raw",
		"resolve",
		slaComplyRawRes,
	).Set(1)
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

	// Registries for each endpoint
	regSummary := prometheus.NewRegistry()
	regIncident := prometheus.NewRegistry()
	regUserRequest := prometheus.NewRegistry()

	// Register metrics for each registry
	regSummary.MustRegister(ticketCount)
	regSummary.MustRegister(slaCompliance)

	regIncident.MustRegister(ticketDetailInfo)
	regUserRequest.MustRegister(ticketDetailInfo)

	// Data holders
	var (
		incidentTickets    []itop.Ticket
		userRequestTickets []itop.Ticket
		muIncident         sync.RWMutex
		muUserRequest      sync.RWMutex
	)

	// Parallel fetchers
	go func() {
		for {
			tickets, _ := itop.FetchTicketsByClass("Incident")
			muIncident.Lock()
			incidentTickets = tickets
			muIncident.Unlock()
			time.Sleep(10 * time.Second)
		}
	}()
	go func() {
		for {
			tickets, _ := itop.FetchTicketsByClass("UserRequest")
			muUserRequest.Lock()
			userRequestTickets = tickets
			muUserRequest.Unlock()
			time.Sleep(10 * time.Second)
		}
	}()
	// Start holiday sync goroutine in parallel
	go itop.SyncHolidaysToFile(
		"holidays.txt",
		10*time.Second,
	)

	// Periodic summary metrics updater
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			allTickets := append([]itop.Ticket{}, incidentTickets...)
			allTickets = append(allTickets, userRequestTickets...)
			updateSummaryMetrics(allTickets)
			<-ticker.C
		}
	}()

	// HTTP Handlers
	http.Handle("/metrics", promhttp.HandlerFor(regSummary, promhttp.HandlerOpts{}))
	http.HandleFunc("/incidents", func(w http.ResponseWriter, r *http.Request) {
		regIncident.Unregister(ticketDetailInfo)
		ticketDetailInfo.Reset()
		muIncident.RLock()
		for _, t := range incidentTickets {
			setTicketDetailMetric(t)
		}
		muIncident.RUnlock()
		regIncident.MustRegister(ticketDetailInfo)
		promhttp.HandlerFor(regIncident, promhttp.HandlerOpts{}).ServeHTTP(w, r)
	})
	http.HandleFunc("/userrequests", func(w http.ResponseWriter, r *http.Request) {
		regUserRequest.Unregister(ticketDetailInfo)
		ticketDetailInfo.Reset()
		muUserRequest.RLock()
		for _, t := range userRequestTickets {
			setTicketDetailMetric(t)
		}
		muUserRequest.RUnlock()
		regUserRequest.MustRegister(ticketDetailInfo)
		promhttp.HandlerFor(regUserRequest, promhttp.HandlerOpts{}).ServeHTTP(w, r)
	})

	fmt.Println("Exporter running on :9100/metrics, /incidents, /userrequests")
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
var ticketDetailInfo = prometheus.NewGaugeVec(
	prometheus.GaugeOpts{
		Name: "itop_ticket_detail_info",
		Help: "Detail info per ticket, with all fields, time metrics in seconds, SLA compliance, and metric type.",
	},
	[]string{
		"id", "ref", "class", "title", "status", "priority", "urgency", "impact",
		"service_name", "servicesubcategory_name", "agent_id_friendlyname", "team_id_friendlyname",
		"start_date", "assignment_date", "resolution_date",
		"time_to_response", "time_to_resolve", "type", "sla_metric", "sla_compliance",
	},
)
