// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016-present Datadog, Inc.

package dependencies

import "testing"

func Test_buildID(t *testing.T) {
	type args struct {
		ns   string
		name string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "ns+name",
			args: args{
				ns:   "bar",
				name: "foo",
			},
			want: "bar/foo",
		},
		{
			name: "name_only",
			args: args{
				name: "foo",
			},
			want: "foo",
		},
		{
			name: "ns_only",
			args: args{
				ns: "bar",
			},
			want: "bar/",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := buildID(tt.args.ns, tt.args.name); got != tt.want {
				t.Errorf("buildID() = %v, want %v", got, tt.want)
			}
		})
	}
}
