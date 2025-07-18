package itop

import (
	"encoding/json"
	"time"
)

type TicketResponse struct {
	Objects map[string]struct {
		Fields struct {
			ID                     string `json:"id"`
			Ref                    string `json:"ref"`
			Title                  string `json:"title"`
			Status                 string `json:"status"`
			Priority               string `json:"priority"`
			Urgency                string `json:"urgency"`
			Impact                 string `json:"impact"`
			ServiceID              string `json:"service_id"`
			ServiceName            string `json:"service_name"`
			ServiceSubcategoryName string `json:"service_subcategory_name"`
			AgentID                string `json:"agent_id"`
			Agent                  string `json:"agent_id_friendlyname"`
			TeamID                 string `json:"team_id"`
			Team                   string `json:"team_id_friendlyname"`
			StartDate              string `json:"start_date"`
			AssignmentDate         string `json:"assignment_date"`
			ResolutionDate         string `json:"resolution_date"`
			TTODeadline            string `json:"tto_deadline"`
			TTRDeadline            string `json:"ttr_deadline"`
			SLATTOPassed           string `json:"sla_tto_passed"`
			SLATTRPassed           string `json:"sla_ttr_passed"`
		} `json:"fields"`
	} `json:"objects"`
}

func ParseTickets(data []byte) ([]Ticket, error) {
	var resp TicketResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	var tickets []Ticket
	for _, obj := range resp.Objects {
		fields := obj.Fields
		startDate, _ := time.Parse("2006-01-02 15:04:05", fields.StartDate)
		assignmentDate, _ := time.Parse("2006-01-02 15:04:05", fields.AssignmentDate)
		resolutionDate, _ := time.Parse("2006-01-02 15:04:05", fields.ResolutionDate)
		ttoDeadline, _ := time.Parse("2006-01-02 15:04:05", fields.TTODeadline)
		ttrDeadline, _ := time.Parse("2006-01-02 15:04:05", fields.TTRDeadline)

		ticket := Ticket{
			ID:                 fields.ID,
			Ref:                fields.Ref,
			Title:              fields.Title,
			Status:             fields.Status,
			Class:              "Incident",
			Service:            fields.ServiceName,
			ServiceSubcategory: fields.ServiceSubcategoryName,
			StartDate:          startDate,
			AssignmentDate:     assignmentDate,
			ResolutionDate:     resolutionDate,
			TTODeadline:        ttoDeadline,
			TTRDeadline:        ttrDeadline,
			SLATTOPassed:       fields.SLATTOPassed,
			SLATTRPassed:       fields.SLATTRPassed,
			Agent:              fields.Agent,
			AgentID:            fields.AgentID,
			Team:               fields.Team,
			TeamID:             fields.TeamID,
			Priority:           fields.Priority,
			Urgency:            fields.Urgency,
			Impact:             fields.Impact,
			ServiceID:          fields.ServiceID,
		}
		// Calculate TTO/TTR
		if !assignmentDate.IsZero() && !startDate.IsZero() {
			ticket.TimeToResponse = assignmentDate.Sub(startDate)
		}
		if !resolutionDate.IsZero() && !startDate.IsZero() {
			ticket.TimeToResolve = resolutionDate.Sub(startDate)
		}
		tickets = append(tickets, ticket)
	}
	return tickets, nil
}
