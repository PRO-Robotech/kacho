// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package auditlistfilter

import (
	"fmt"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"

	authzv1 "github.com/PRO-Robotech/corelib/api/corelib/authz/v1"

	// The compiled notify contract registers its descriptors here: the options are
	// read from what the server is built from, not from the .proto text.
	_ "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/notify/v1"
)

// compiledRPCAuthz reads the authorization options of a method linked into this
// binary.
func compiledRPCAuthz(fullName string) (RPCAuthz, error) {
	d, err := protoregistry.GlobalFiles.FindDescriptorByName(protoreflect.FullName(fullName))
	if err != nil {
		return RPCAuthz{}, err
	}
	md, ok := d.(protoreflect.MethodDescriptor)
	if !ok {
		return RPCAuthz{}, fmt.Errorf("%s is not a method", fullName)
	}
	opts, ok := md.Options().(*descriptorpb.MethodOptions)
	if !ok || opts == nil {
		return RPCAuthz{}, nil
	}
	out := RPCAuthz{}
	if v, ok := proto.GetExtension(opts, authzv1.E_Permission).(string); ok {
		out.Permission = v
	}
	if v, ok := proto.GetExtension(opts, authzv1.E_RequiredRelation).(string); ok {
		out.RequiredRelation = v
	}
	if se, ok := proto.GetExtension(opts, authzv1.E_ScopeExtractor).(*authzv1.ScopeExtractor); ok && se != nil {
		out.ScopeObjectType = se.GetObjectType()
		out.ScopeField = se.GetFromRequestField()
	}
	return out, nil
}
