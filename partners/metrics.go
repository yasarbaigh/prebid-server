package partners

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	SSPRequestCounter = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "rtb_ssp_requests_total",
			Help: "Total number of RTB requests received from SSPs.",
		},
		[]string{"prometheus_identifier", "tenant_id"},
	)

	DSPRequestCounter = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "rtb_dsp_requests_total",
			Help: "Total number of RTB requests fanned out to DSPs.",
		},
		[]string{"prometheus_identifier", "tenant_id"},
	)

	AuctionCounter = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "rtb_auctions_total",
			Help: "Total number of RTB auctions conducted.",
		},
		[]string{"status"}, // ok, rejected_tmax, rejected_adserving_disabled
	)
)
