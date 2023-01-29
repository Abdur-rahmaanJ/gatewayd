package plugin

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/structpb"
)

func Test_NewHookConfig(t *testing.T) {
	hc := NewHookConfig()
	assert.NotNil(t, hc)
}

func Test_HookConfig_Add(t *testing.T) {
	hooks := NewHookConfig()
	testFunc := func(
		ctx context.Context,
		args *structpb.Struct,
		opts ...grpc.CallOption,
	) (*structpb.Struct, error) {
		return args, nil
	}
	hooks.Add(OnNewLogger, 0, testFunc)
	assert.NotNil(t, hooks.Hooks()[OnNewLogger][0])
	assert.ObjectsAreEqual(testFunc, hooks.Hooks()[OnNewLogger][0])
}

func Test_HookConfig_Add_Multiple_Hooks(t *testing.T) {
	hooks := NewHookConfig()
	hooks.Add(OnNewLogger, 0, func(
		ctx context.Context,
		args *structpb.Struct,
		opts ...grpc.CallOption,
	) (*structpb.Struct, error) {
		return args, nil
	})
	hooks.Add(OnNewLogger, 1, func(
		ctx context.Context,
		args *structpb.Struct,
		opts ...grpc.CallOption,
	) (*structpb.Struct, error) {
		return args, nil
	})
	assert.NotNil(t, hooks.Hooks()[OnNewLogger][0])
	assert.NotNil(t, hooks.Hooks()[OnNewLogger][1])
}

func Test_HookConfig_Get(t *testing.T) {
	hooks := NewHookConfig()
	testFunc := func(
		ctx context.Context,
		args *structpb.Struct,
		opts ...grpc.CallOption,
	) (*structpb.Struct, error) {
		return args, nil
	}
	prio := Priority(0)
	hooks.Add(OnNewLogger, prio, testFunc)
	assert.NotNil(t, hooks.Get(OnNewLogger))
	assert.ObjectsAreEqual(testFunc, hooks.Get(OnNewLogger)[prio])
}

func Test_HookConfig_Run(t *testing.T) {
	hooks := NewHookConfig()
	hooks.Add(OnNewLogger, 0, func(
		ctx context.Context,
		args *structpb.Struct,
		opts ...grpc.CallOption,
	) (*structpb.Struct, error) {
		return args, nil
	})
	result, err := hooks.Run(context.Background(), map[string]interface{}{}, OnNewLogger, Ignore)
	assert.NotNil(t, result)
	assert.Nil(t, err)
}

func Test_HookConfig_Run_PassDown(t *testing.T) {
	hooks := NewHookConfig()
	// The result of the hook will be nil and will be passed down to the next hook.
	hooks.Add(OnNewLogger, 0, func(
		ctx context.Context,
		args *structpb.Struct,
		opts ...grpc.CallOption,
	) (*structpb.Struct, error) {
		return nil, nil //nolint:nilnil
	})
	// The consolidated result should be {"test": "test"}.
	hooks.Add(OnNewLogger, 1, func(
		ctx context.Context,
		args *structpb.Struct,
		opts ...grpc.CallOption,
	) (*structpb.Struct, error) {
		output, err := structpb.NewStruct(map[string]interface{}{
			"test": "test",
		})
		assert.Nil(t, err)
		return output, nil
	})

	// Although the first hook returns nil, and its signature doesn't match the params,
	// so its result (nil) is passed down to the next hook in chain (prio 2).
	// Then the second hook runs and returns a signature with a "test" key and value.
	result, err := hooks.Run(
		context.Background(),
		map[string]interface{}{"test": "test"},
		OnNewLogger,
		PassDown)
	assert.Nil(t, err)
	assert.NotNil(t, result)
}

func Test_HookConfig_Run_PassDown_2(t *testing.T) {
	hooks := NewHookConfig()
	// The result of the hook will be nil and will be passed down to the next hook.
	hooks.Add(OnNewLogger, 0, func(
		ctx context.Context,
		args *structpb.Struct,
		opts ...grpc.CallOption,
	) (*structpb.Struct, error) {
		args.Fields["test1"] = &structpb.Value{
			Kind: &structpb.Value_StringValue{ //nolint:nosnakecase
				StringValue: "test1",
			},
		}
		return args, nil
	})
	// The consolidated result should be {"test1": "test1", "test2": "test2"}.
	hooks.Add(OnNewLogger, 1, func(
		ctx context.Context,
		args *structpb.Struct,
		opts ...grpc.CallOption,
	) (*structpb.Struct, error) {
		args.Fields["test2"] = &structpb.Value{
			Kind: &structpb.Value_StringValue{ //nolint:nosnakecase
				StringValue: "test2",
			},
		}
		return args, nil
	})
	// Although the first hook returns nil, and its signature doesn't match the params,
	// so its result (nil) is passed down to the next hook in chain (prio 2).
	// Then the second hook runs and returns a signature with a "test1" and "test2" key and value.
	result, err := hooks.Run(
		context.Background(),
		map[string]interface{}{"test": "test"},
		OnNewLogger,
		PassDown)
	assert.Nil(t, err)
	assert.NotNil(t, result)
}

func Test_HookConfig_Run_Ignore(t *testing.T) {
	hooks := NewHookConfig()
	// This should not run, because the return value is not the same as the params
	hooks.Add(OnNewLogger, 0, func(
		ctx context.Context,
		args *structpb.Struct,
		opts ...grpc.CallOption,
	) (*structpb.Struct, error) {
		return nil, nil //nolint:nilnil
	})
	// This should run, because the return value is the same as the params
	hooks.Add(OnNewLogger, 1, func(
		ctx context.Context,
		args *structpb.Struct,
		opts ...grpc.CallOption,
	) (*structpb.Struct, error) {
		args.Fields["test"] = &structpb.Value{
			Kind: &structpb.Value_StringValue{ //nolint:nosnakecase
				StringValue: "test",
			},
		}
		return args, nil
	})
	// The first hook returns nil, and its signature doesn't match the params,
	// so its result is ignored.
	// Then the second hook runs and returns a signature with a "test" key and value.
	result, err := hooks.Run(
		context.Background(),
		map[string]interface{}{"test": "test"},
		OnNewLogger,
		Ignore)
	assert.Nil(t, err)
	assert.NotNil(t, result)
}

func Test_HookConfig_Run_Abort(t *testing.T) {
	hooks := NewHookConfig()
	// This should not run, because the return value is not the same as the params
	hooks.Add(OnNewLogger, 0, func(
		ctx context.Context,
		args *structpb.Struct,
		opts ...grpc.CallOption,
	) (*structpb.Struct, error) {
		return nil, nil //nolint:nilnil
	})
	// This should not run, because the first hook returns nil, and its result is ignored.
	hooks.Add(OnNewLogger, 1, func(
		ctx context.Context,
		args *structpb.Struct,
		opts ...grpc.CallOption,
	) (*structpb.Struct, error) {
		output, err := structpb.NewStruct(map[string]interface{}{
			"test": "test",
		})
		assert.Nil(t, err)
		return output, nil
	})
	// The first hook returns nil, and it aborts the execution of the rest of the hook.
	result, err := hooks.Run(context.Background(), map[string]interface{}{}, OnNewLogger, Abort)
	assert.Nil(t, err)
	assert.Equal(t, map[string]interface{}{}, result)
}

func Test_HookConfig_Run_Remove(t *testing.T) {
	hooks := NewHookConfig()
	// This should not run, because the return value is not the same as the params
	hooks.Add(OnNewLogger, 0, func(
		ctx context.Context,
		args *structpb.Struct,
		opts ...grpc.CallOption,
	) (*structpb.Struct, error) {
		return nil, nil //nolint:nilnil
	})
	// This should not run, because the first hook returns nil, and its result is ignored.
	hooks.Add(OnNewLogger, 1, func(
		ctx context.Context,
		args *structpb.Struct,
		opts ...grpc.CallOption,
	) (*structpb.Struct, error) {
		output, err := structpb.NewStruct(map[string]interface{}{
			"test": "test",
		})
		assert.Nil(t, err)
		return output, nil
	})
	// The first hook returns nil, and its signature doesn't match the params,
	// so its result is ignored. The failing hook is removed from the list and
	// the execution continues with the next hook in the list.
	result, err := hooks.Run(context.Background(), map[string]interface{}{}, OnNewLogger, Remove)
	assert.Nil(t, err)
	assert.Equal(t, map[string]interface{}{}, result)
	assert.Equal(t, 1, len(hooks.Hooks()[OnNewLogger]))
}