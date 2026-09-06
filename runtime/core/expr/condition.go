package expr

import (
	"fmt"

	"github.com/juancavallotti/octo/runtime/types"
)

// EvalCondition evaluates a boolean expression against the message, erroring if
// the result is not a bool. It is the one evaluator every guard in the runtime
// shares — an if's condition, a switch's case, a validate rule, a mock case — so
// "must evaluate to a bool" is worded once.
func EvalCondition(program *Program, msg *types.Message, env map[string]any) (bool, error) {
	value, err := program.Eval(MessageActivation(msg, env))
	if err != nil {
		return false, err
	}
	result, ok := value.(bool)
	if !ok {
		return false, fmt.Errorf("condition must evaluate to a bool, got %T", value)
	}
	return result, nil
}
