package tarantool

import (
	"context"
	"fmt"
)

// the Result type is used to return write errors here
func (conn *Connection) writeRequest(ctx context.Context, q Query, replyChan chan *AsyncResult) (*request, *Result) {
	var err error

	requestID := conn.nextID()
	request := &request{
		replyChan: replyChan,
	}

	pp := packetPool.GetWithID(requestID)

	if err = pp.packMsg(q, conn.packData); err != nil {
		return nil, &Result{
			Error:     NewQueryError(ErrInvalidMsgpack, err.Error()),
			ErrorCode: ErrInvalidMsgpack,
		}
	}

	if oldRequest := conn.requests.Put(requestID, request); oldRequest != nil {
		select {
		case oldRequest.replyChan <- &AsyncResult{
			Error:     ConnectionClosedError(conn),
			ErrorCode: ErrNoConnection,
		}:
		default:
		}
	}

	writeChan := conn.writeChan
	if writeChan == nil {
		return nil, &Result{
			Error:     ConnectionClosedError(conn),
			ErrorCode: ErrNoConnection,
		}
	}

	select {
	case writeChan <- pp:
	case <-ctx.Done():
		if conn.perf.QueryTimeouts != nil {
			conn.perf.QueryTimeouts.Add(1)
		}
		conn.requests.Pop(requestID)
		return nil, &Result{
			Error:     NewContextError(ctx, conn, "Send error"),
			ErrorCode: ErrTimeout,
		}
	case <-conn.exit:
		return nil, &Result{
			Error:     ConnectionClosedError(conn),
			ErrorCode: ErrNoConnection,
		}
	}

	return request, nil
}

func (conn *Connection) readResult(ctx context.Context, arc chan *AsyncResult) *AsyncResult {
	select {
	case ar := <-arc:
		if ar == nil {
			return &AsyncResult{
				Error:     ConnectionClosedError(conn),
				ErrorCode: ErrNoConnection,
			}
		}
		return ar
	case <-ctx.Done():
		if conn.perf.QueryTimeouts != nil {
			conn.perf.QueryTimeouts.Add(1)
		}
		return &AsyncResult{
			Error:     NewContextError(ctx, conn, "Recv error"),
			ErrorCode: ErrTimeout,
		}
	case <-conn.exit:
		return &AsyncResult{
			Error:     ConnectionClosedError(conn),
			ErrorCode: ErrNoConnection,
		}
	}
}

func (conn *Connection) Exec(ctx context.Context, q Query) (result *Result) {
	var cancel context.CancelFunc = func() {}

	if _, ok := ctx.Deadline(); !ok && conn.queryTimeout != 0 {
		ctx, cancel = context.WithTimeout(ctx, conn.queryTimeout)
	}

	replyChan := make(chan *AsyncResult, 1)

	if _, rerr := conn.writeRequest(ctx, q, replyChan); rerr != nil {
		cancel()
		return rerr
	}

	ar := conn.readResult(ctx, replyChan)
	cancel()

	if rerr := ar.Error; rerr != nil {
		return &Result{
			Error:     rerr,
			ErrorCode: ar.ErrorCode,
		}
	}

	pp := ar.BinaryPacket
	if pp == nil {
		return &Result{
			Error:     ConnectionClosedError(conn),
			ErrorCode: ErrNoConnection,
		}
	}

	if err := pp.packet.UnmarshalBinary(pp.body); err != nil {
		result = &Result{
			Error:     fmt.Errorf("Error decoding packet type %d: %s", pp.packet.Cmd, err),
			ErrorCode: ErrInvalidMsgpack,
		}
	} else {
		result = pp.Result()
		if result == nil {
			result = &Result{}
		}
	}
	pp.Release()

	return result
}

func (conn *Connection) ExecAsync(ctx context.Context, q Query, replyChan chan *AsyncResult) (result *Result) {
	if _, rerr := conn.writeRequest(ctx, q, replyChan); rerr != nil {
		return rerr
	}
	return nil
}

func (conn *Connection) Execute(q Query) ([][]interface{}, error) {
	res := conn.Exec(context.Background(), q)
	return res.Data, res.Error
}
