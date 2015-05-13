#!/usr/bin/env python

# Copyright 2014 Google Inc. All Rights Reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

import ipdb
import fixup_extensions
import google.fakes.gateway_pb2 as pb

_TIMEOUT = 10 #s
#ipdb.set_trace()

with pb.early_adopter_create_Gateway_stub('localhost', 10000) as stub:

  # register a fake
  req = pb.RegisterRequest()
  req.registration.target_pattern = r'/my_fake/.*'
  req.registration.resolved_target = 'http://localhost:10001'
  resp = stub.Register(req, _TIMEOUT)
  print('Register successful')
  print(resp)

  # rerequest it
  req = pb.ResolveRequest()
  req.target = '/my_fake/api/v2/test'
  resp = stub.Resolve(req, _TIMEOUT)
  print('Resolve 1 successful')
  print(resp)

  # request something else
  req = pb.ResolveRequest()
  req.target = '/my_blah/foo'
  resp = stub.Resolve(req, _TIMEOUT)
  print('Resolve 2 successful')
  print(resp)

