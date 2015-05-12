#!/usr/bin/env python
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

