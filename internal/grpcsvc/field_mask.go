package grpcsvc

import (
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

func validateFieldMask(mask *fieldmaskpb.FieldMask, message proto.Message) error {
	if mask != nil && (len(mask.GetPaths()) == 0 || !mask.IsValid(message)) {
		return status.Errorf(codes.InvalidArgument, "invalid field mask for %s", message.ProtoReflect().Descriptor().Name())
	}
	return nil
}

// keepFields applies validated paths to a detached response message.
func keepFields(message protoreflect.Message, paths []string) {
	if !message.IsValid() {
		return
	}
	message.Range(func(field protoreflect.FieldDescriptor, value protoreflect.Value) bool {
		var nested []string
		for _, path := range paths {
			name, rest, hasChild := strings.Cut(path, ".")
			if protoreflect.Name(name) != field.Name() {
				continue
			}
			if !hasChild {
				return true
			}
			nested = append(nested, rest)
		}
		if len(nested) == 0 {
			message.Clear(field)
		} else {
			keepFields(value.Message(), nested)
		}
		return true
	})
	message.SetUnknown(nil)
}
