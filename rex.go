package ergo

// https://github.com/erlang/otp/blob/master/lib/kernel/src/rpc.erl

import (
	"fmt"

	"github.com/halturin/ergo/etf"
	"github.com/halturin/ergo/lib"
)

type rpcFunction func(...etf.Term) etf.Term

type modFun struct {
	module   string
	function string
}

var (
	allowedModFun = []string{
		"observer_backend",
	}
)

type rex struct {
	GenServer
	process *Process
	methods map[modFun]rpcFunction
}

// Init initializes process state using arbitrary arguments
// Init(...) -> state
func (r *rex) Init(p *Process, args ...interface{}) (state interface{}) {
	lib.Log("REX: Init: %#v", args)
	r.process = p
	r.methods = make(map[modFun]rpcFunction, 0)

	for i := range allowedModFun {
		mf := modFun{
			allowedModFun[i],
			"*",
		}
		r.methods[mf] = nil
	}

	return nil
}

// HandleCast -> ("noreply", state) - noreply
//		         ("stop", reason) - stop with reason
func (r *rex) HandleCast(message etf.Term, state interface{}) (string, interface{}) {
	lib.Log("REX: HandleCast: %#v", message)
	return "noreply", state
}

// HandleCall serves incoming messages sending via gen_server:call
// HandleCall -> ("reply", message, state) - reply
//				 ("noreply", _, state) - noreply
//		         ("stop", reason, _) - normal stop
func (r *rex) HandleCall(from etf.Tuple, message etf.Term, state interface{}) (string, etf.Term, interface{}) {
	lib.Log("REX: HandleCall: %#v, From: %#v", message, from)
	switch m := message.(type) {
	case etf.Tuple:
		//etf.Tuple{"call", "observer_backend", "sys_info",
		//           etf.List{}, etf.Pid{Node:"erl-examplenode@127.0.0.1", Id:0x46, Serial:0x0, Creation:0x2}}
		switch m.Element(1) {
		case etf.Atom("call"):
			module := m.Element(2).(etf.Atom)
			function := m.Element(3).(etf.Atom)
			args := m.Element(4).(etf.List)
			reply, state1 := r.handleRPC(module, function, args, state)
			if reply != nil {
				return "reply", reply, state1
			}

			to := etf.Tuple{string(module), r.process.Node.FullName}
			m := etf.Tuple{m.Element(3), m.Element(4)}
			reply, err := r.process.Call(to, m)
			if err != nil {
				reply = etf.Term(etf.Tuple{etf.Atom("error"), err})
			}
			return "reply", reply, state

		case etf.Atom("$provide"):
			module := m.Element(2).(etf.Atom)
			function := m.Element(3).(etf.Atom)
			fun := m.Element(4).(rpcFunction)
			mf := modFun{
				module:   string(module),
				function: string(function),
			}
			if _, ok := r.methods[mf]; ok {
				return "reply", etf.Atom("taken"), state
			}

			r.methods[mf] = fun
			return "reply", etf.Atom("ok"), state

		case etf.Atom("$revoke"):
			module := m.Element(2).(etf.Atom)
			function := m.Element(3).(etf.Atom)
			mf := modFun{
				module:   string(module),
				function: string(function),
			}

			if _, ok := r.methods[mf]; ok {
				delete(r.methods, mf)
				return "reply", etf.Atom("ok"), state
			}

			return "reply", etf.Atom("unknown"), state
		}

	}

	reply := etf.Term(etf.Tuple{etf.Atom("badrpc"), etf.Atom("unknown")})
	return "reply", reply, state
}

// HandleInfo serves all another incoming messages (Pid ! message)
// HandleInfo -> ("noreply", state) - noreply
//		         ("stop", reason) - normal stop
func (r *rex) HandleInfo(message etf.Term, state interface{}) (string, interface{}) {
	lib.Log("REX: HandleInfo: %#v", message)
	return "noreply", state
}

// Terminate called when process died
func (r *rex) Terminate(reason string, state interface{}) {
	lib.Log("REX: Terminate: %#v", reason)
}

func (r *rex) handleRPC(module, function etf.Atom, args etf.List, state interface{}) (reply, state1 interface{}) {
	defer func() {
		if x := recover(); x != nil {
			err := fmt.Sprintf("panic reason: %s", x)
			// recovered
			reply = etf.Tuple{
				etf.Atom("badrpc"),
				etf.Tuple{
					etf.Atom("EXIT"),
					etf.Tuple{
						etf.Atom("panic"),
						etf.List{
							etf.Tuple{module, function, args, etf.List{err}},
						},
					},
				},
			}
		}
	}()
	state1 = state
	mf := modFun{
		module:   string(module),
		function: string(function),
	}
	// calling dynamically declared rpc method
	if function, ok := r.methods[mf]; ok {
		reply = function(args...)
		return
	}

	// calling a local module if its been registered as a process)
	if r.process.Node.GetProcessByName(mf.module) != nil {
		return nil, state
	}

	// unknown request. return error
	reply = etf.Tuple{
		etf.Atom("badrpc"),
		etf.Tuple{
			etf.Atom("EXIT"),
			etf.Tuple{
				etf.Atom("undef"),
				etf.List{
					etf.Tuple{
						module,
						function,
						args,
						etf.List{},
					},
				},
			},
		},
	}

	return reply, state
}
