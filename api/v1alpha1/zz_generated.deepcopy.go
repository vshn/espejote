//go:build !ignore_autogenerated

// Code generated by controller-gen. DO NOT EDIT.

package v1alpha1

import (
	"k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
)

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Admission) DeepCopyInto(out *Admission) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Admission.
func (in *Admission) DeepCopy() *Admission {
	if in == nil {
		return nil
	}
	out := new(Admission)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *Admission) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *AdmissionList) DeepCopyInto(out *AdmissionList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]Admission, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new AdmissionList.
func (in *AdmissionList) DeepCopy() *AdmissionList {
	if in == nil {
		return nil
	}
	out := new(AdmissionList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *AdmissionList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *AdmissionSpec) DeepCopyInto(out *AdmissionSpec) {
	*out = *in
	in.WebhookConfiguration.DeepCopyInto(&out.WebhookConfiguration)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new AdmissionSpec.
func (in *AdmissionSpec) DeepCopy() *AdmissionSpec {
	if in == nil {
		return nil
	}
	out := new(AdmissionSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ApplyOptions) DeepCopyInto(out *ApplyOptions) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ApplyOptions.
func (in *ApplyOptions) DeepCopy() *ApplyOptions {
	if in == nil {
		return nil
	}
	out := new(ApplyOptions)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ContextResource) DeepCopyInto(out *ContextResource) {
	*out = *in
	if in.Namespace != nil {
		in, out := &in.Namespace, &out.Namespace
		*out = new(string)
		**out = **in
	}
	if in.LabelSelector != nil {
		in, out := &in.LabelSelector, &out.LabelSelector
		*out = new(metav1.LabelSelector)
		(*in).DeepCopyInto(*out)
	}
	if in.MatchNames != nil {
		in, out := &in.MatchNames, &out.MatchNames
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.IgnoreNames != nil {
		in, out := &in.IgnoreNames, &out.IgnoreNames
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.StripManagedFields != nil {
		in, out := &in.StripManagedFields, &out.StripManagedFields
		*out = new(bool)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ContextResource.
func (in *ContextResource) DeepCopy() *ContextResource {
	if in == nil {
		return nil
	}
	out := new(ContextResource)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *JsonnetLibrary) DeepCopyInto(out *JsonnetLibrary) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new JsonnetLibrary.
func (in *JsonnetLibrary) DeepCopy() *JsonnetLibrary {
	if in == nil {
		return nil
	}
	out := new(JsonnetLibrary)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *JsonnetLibrary) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *JsonnetLibraryList) DeepCopyInto(out *JsonnetLibraryList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]JsonnetLibrary, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new JsonnetLibraryList.
func (in *JsonnetLibraryList) DeepCopy() *JsonnetLibraryList {
	if in == nil {
		return nil
	}
	out := new(JsonnetLibraryList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *JsonnetLibraryList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *JsonnetLibrarySpec) DeepCopyInto(out *JsonnetLibrarySpec) {
	*out = *in
	if in.Data != nil {
		in, out := &in.Data, &out.Data
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new JsonnetLibrarySpec.
func (in *JsonnetLibrarySpec) DeepCopy() *JsonnetLibrarySpec {
	if in == nil {
		return nil
	}
	out := new(JsonnetLibrarySpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ManagedResource) DeepCopyInto(out *ManagedResource) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	out.Status = in.Status
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ManagedResource.
func (in *ManagedResource) DeepCopy() *ManagedResource {
	if in == nil {
		return nil
	}
	out := new(ManagedResource)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *ManagedResource) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ManagedResourceContext) DeepCopyInto(out *ManagedResourceContext) {
	*out = *in
	in.Resource.DeepCopyInto(&out.Resource)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ManagedResourceContext.
func (in *ManagedResourceContext) DeepCopy() *ManagedResourceContext {
	if in == nil {
		return nil
	}
	out := new(ManagedResourceContext)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ManagedResourceList) DeepCopyInto(out *ManagedResourceList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]ManagedResource, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ManagedResourceList.
func (in *ManagedResourceList) DeepCopy() *ManagedResourceList {
	if in == nil {
		return nil
	}
	out := new(ManagedResourceList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *ManagedResourceList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ManagedResourceSpec) DeepCopyInto(out *ManagedResourceSpec) {
	*out = *in
	if in.Triggers != nil {
		in, out := &in.Triggers, &out.Triggers
		*out = make([]ManagedResourceTrigger, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.Context != nil {
		in, out := &in.Context, &out.Context
		*out = make([]ManagedResourceContext, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	out.ServiceAccountRef = in.ServiceAccountRef
	out.ApplyOptions = in.ApplyOptions
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ManagedResourceSpec.
func (in *ManagedResourceSpec) DeepCopy() *ManagedResourceSpec {
	if in == nil {
		return nil
	}
	out := new(ManagedResourceSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ManagedResourceStatus) DeepCopyInto(out *ManagedResourceStatus) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ManagedResourceStatus.
func (in *ManagedResourceStatus) DeepCopy() *ManagedResourceStatus {
	if in == nil {
		return nil
	}
	out := new(ManagedResourceStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ManagedResourceTrigger) DeepCopyInto(out *ManagedResourceTrigger) {
	*out = *in
	out.Interval = in.Interval
	in.WatchResource.DeepCopyInto(&out.WatchResource)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ManagedResourceTrigger.
func (in *ManagedResourceTrigger) DeepCopy() *ManagedResourceTrigger {
	if in == nil {
		return nil
	}
	out := new(ManagedResourceTrigger)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *TriggerWatchResource) DeepCopyInto(out *TriggerWatchResource) {
	*out = *in
	if in.Namespace != nil {
		in, out := &in.Namespace, &out.Namespace
		*out = new(string)
		**out = **in
	}
	if in.LabelSelector != nil {
		in, out := &in.LabelSelector, &out.LabelSelector
		*out = new(metav1.LabelSelector)
		(*in).DeepCopyInto(*out)
	}
	if in.MatchNames != nil {
		in, out := &in.MatchNames, &out.MatchNames
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.IgnoreNames != nil {
		in, out := &in.IgnoreNames, &out.IgnoreNames
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.StripManagedFields != nil {
		in, out := &in.StripManagedFields, &out.StripManagedFields
		*out = new(bool)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new TriggerWatchResource.
func (in *TriggerWatchResource) DeepCopy() *TriggerWatchResource {
	if in == nil {
		return nil
	}
	out := new(TriggerWatchResource)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *WebhookConfiguration) DeepCopyInto(out *WebhookConfiguration) {
	*out = *in
	if in.Rules != nil {
		in, out := &in.Rules, &out.Rules
		*out = make([]v1.RuleWithOperations, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.FailurePolicy != nil {
		in, out := &in.FailurePolicy, &out.FailurePolicy
		*out = new(v1.FailurePolicyType)
		**out = **in
	}
	if in.MatchPolicy != nil {
		in, out := &in.MatchPolicy, &out.MatchPolicy
		*out = new(v1.MatchPolicyType)
		**out = **in
	}
	if in.ObjectSelector != nil {
		in, out := &in.ObjectSelector, &out.ObjectSelector
		*out = new(metav1.LabelSelector)
		(*in).DeepCopyInto(*out)
	}
	if in.ReinvocationPolicy != nil {
		in, out := &in.ReinvocationPolicy, &out.ReinvocationPolicy
		*out = new(v1.ReinvocationPolicyType)
		**out = **in
	}
	if in.MatchConditions != nil {
		in, out := &in.MatchConditions, &out.MatchConditions
		*out = make([]v1.MatchCondition, len(*in))
		copy(*out, *in)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new WebhookConfiguration.
func (in *WebhookConfiguration) DeepCopy() *WebhookConfiguration {
	if in == nil {
		return nil
	}
	out := new(WebhookConfiguration)
	in.DeepCopyInto(out)
	return out
}
