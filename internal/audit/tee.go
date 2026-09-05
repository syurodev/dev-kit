package audit

type teeSink struct{ sinks []Sink }

// NewTee returns a Sink that records every event to each of sinks, in order.
func NewTee(sinks ...Sink) Sink { return teeSink{sinks: sinks} }

func (t teeSink) Record(event Event) {
	for _, s := range t.sinks {
		s.Record(event)
	}
}
