package tnt

import "time"

type QueryOptions struct {
	Timeout time.Duration
}

func (conn *Connection) doExecute(q Query, deadline <-chan time.Time, abort chan bool) ([]interface{}, error) {
	request := &request{
		query:     q,
		replyChan: make(chan *Response, 1),
	}

	select {
	case conn.requestChan <- request:
		// pass
	case <-deadline:
		return nil, NewConnectionError("Request send timeout")
	case <-abort:
		return nil, NewConnectionError("Request aborted")
	case <-conn.exit:
		return nil, ConnectionClosedError()
	}

	var response *Response
	select {
	case response = <-request.replyChan:
		// pass
	case <-deadline:
		return nil, NewConnectionError("Response read timeout")
	case <-abort:
		return nil, NewConnectionError("Request aborted")
	case <-conn.exit:
		return nil, ConnectionClosedError()
	}

	return response.Data, response.Error
}

func (conn *Connection) ExecuteOptions(q Query, opts *QueryOptions) ([]interface{}, error) {
	// make options
	if opts == nil {
		opts = &QueryOptions{}
	}

	if opts.Timeout.Nanoseconds() == 0 {
		opts.Timeout = conn.queryTimeout
	}

	// set execute deadline
	deadline := time.After(opts.Timeout)

	return conn.doExecute(q, deadline, nil)
}

func (conn *Connection) Execute(q Query) (result []interface{}, err error) {
	return conn.ExecuteOptions(q, nil)
}
