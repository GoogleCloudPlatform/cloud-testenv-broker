from pkgutil import extend_path
import os
import google
here = os.path.abspath(os.path.dirname(__file__))
google.__path__ = extend_path(google.__path__, os.path.join(here, 'google'))
import google.protobuf as protobuf
protobuf.__path__ = extend_path(protobuf.__path__, os.path.join(here, 'google/protobuf'))
print('module extensions installed')
