package partners

import (
	"github.com/prometheus/client_golang/prometheus"
)

var (
	SSPRequestCounter      *prometheus.CounterVec
	SSPResponseCounter     *prometheus.CounterVec
	DSPRequestCounter      *prometheus.CounterVec
	DSPResponseCounter     *prometheus.CounterVec
	DSPLatencyHistogram    *prometheus.HistogramVec
	AuctionCounter         *prometheus.CounterVec
	ExchangeProfitCounter  *prometheus.CounterVec
	ExchangeRevenueCounter *prometheus.CounterVec
	ExchangeSpentCounter   *prometheus.CounterVec
)

func init() {
	// Initialize with a dummy registry by default to prevent nil pointer panics
	InitMetrics(prometheus.NewRegistry())
}

func InitMetrics(reg prometheus.Registerer) {
	SSPRequestCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "rtb_ssp_requests_total",
			Help: "Total number of RTB requests received from SSPs.",
		},
		[]string{"prometheus_identifier", "tenant_identifier", "ssp_identifier"},
	)
	reg.MustRegister(SSPRequestCounter)

	SSPResponseCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "rtb_ssp_responses_total",
			Help: "Total number of RTB responses sent back to SSPs.",
		},
		[]string{"prometheus_identifier", "tenant_identifier", "ssp_identifier", "status", "http_code"}, // status: ok, no_bid, error
	)
	reg.MustRegister(SSPResponseCounter)

	DSPRequestCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "rtb_dsp_requests_total",
			Help: "Total number of RTB requests fanned out to DSPs.",
		},
		[]string{"prometheus_identifier", "tenant_identifier", "dsp_identifier"},
	)
	reg.MustRegister(DSPRequestCounter)

	DSPResponseCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "rtb_dsp_responses_total",
			Help: "Total number of RTB responses received from DSPs.",
		},
		[]string{"prometheus_identifier", "tenant_identifier", "dsp_identifier", "status", "http_code"}, // status: bid, nobid, error
	)
	reg.MustRegister(DSPResponseCounter)

	DSPLatencyHistogram = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "rtb_dsp_latency_seconds",
			Help:    "Latency of RTB responses from DSPs in seconds.",
			Buckets: []float64{0.01, 0.02, 0.05, 0.1, 0.2, 0.3, 0.4, 0.5},
		},
		[]string{"prometheus_identifier", "tenant_identifier", "dsp_identifier"},
	)
	reg.MustRegister(DSPLatencyHistogram)

	AuctionCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "rtb_auctions_total",
			Help: "Total number of RTB auctions conducted.",
		},
		[]string{"status"}, // ok, rejected_tmax, rejected_adserving_disabled
	)
	reg.MustRegister(AuctionCounter)

	ExchangeProfitCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "rtb_exchange_profit_total",
			Help: "Total profit made by the exchange.",
		},
		[]string{"ssp_identifier", "dsp_identifier", "tenant_identifier"},
	)
	reg.MustRegister(ExchangeProfitCounter)

	ExchangeRevenueCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "rtb_exchange_revenue_total",
			Help: "Total revenue paid by DSPs.",
		},
		[]string{"ssp_identifier", "dsp_identifier", "tenant_identifier"},
	)
	reg.MustRegister(ExchangeRevenueCounter)

	ExchangeSpentCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "rtb_exchange_spent_total",
			Help: "Total amount paid to SSP/Publishers.",
		},
		[]string{"ssp_identifier", "dsp_identifier", "tenant_identifier"},
	)
	reg.MustRegister(ExchangeSpentCounter)
}
