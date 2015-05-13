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

from pkgutil import extend_path
import os
import google
import google_ext
here = os.path.abspath(os.path.dirname(__file__))
google.__path__ = extend_path(google.__path__, os.path.join(here, 'google_ext'))
import google.protobuf as protobuf
protobuf.__path__ = extend_path(protobuf.__path__, os.path.join(here, 'google_ext/protobuf'))
print('module extensions installed')
