from pkgutil import extend_path
import os
import google
import google_ext
here = os.path.abspath(os.path.dirname(__file__))
google.__path__ = extend_path(google.__path__, os.path.join(here, 'google_ext'))
import google.protobuf as protobuf
protobuf.__path__ = extend_path(protobuf.__path__, os.path.join(here, 'google_ext/protobuf'))
print('module extensions installed')
