# Installation steps
1. Lua is a wrapper SDK that depends on the C++ Server SDK. The following dependencies assume you will build the C++ Server SDK from source. If you already have the C+ Server SDK installed, only LuaRocks is required.

First, ensure the required build dependencies are installed:
- [LuaRocks](https://luarocks.org/)
- C++ 17
- [CMake 3.19 or higher](https://cmake.org/)
- A build system such as [make](https://www.gnu.org/software/make/manual/make.html), [Ninja](https://ninja-build.org/), or [MSVC](https://visualstudio.microsoft.com/)
- [Boost 1.81](https://www.boost.org/) or higher
- [OpenSSL](https://www.openssl.org/)

2. If the C++ Server SDK is already installed or you already obtained [release artifacts](https://github.com/launchdarkly/cpp-sdks/releases?q="launchdarkly-cpp-server") from LaunchDarkly, skip to step 3.

Otherwise, compile and install the C++ Server SDK:
```shell
git clone https://github.com/launchdarkly/cpp-sdks.git && cd cpp-sdks
mkdir build && cd build
cmake -G Ninja -D BUILD_TESTING=OFF \
               -D CMAKE_BUILD_TYPE=Release \
               -D LD_BUILD_SHARED_LIBS=On \
               -D CMAKE_INSTALL_PREFIX=./install ..
cmake --build . --target launchdarkly-cpp-server
cmake --install .
cd ../../
```

3. Download the Lua Server SDK and build it with `luarocks` (replace `LD_DIR` with the path to the C++ SDK's shared libraries as necessary):
```shell
luarocks install launchdarkly-server-sdk LD_DIR="$(pwd)/cpp-sdks/build/install"
```

4. Create a file named hello.lua and add the following code:
```lua
local ld = require("launchdarkly_server_sdk")
local config = {}

local client = ld.clientInit("1234567890abcdef", 1000, config)

local user = ld.makeContext({
    user = {
        key = "example-user-key",
        name = "Sandy"
    }
})

local value = client:boolVariation(user, "my-flag-key", false)
print("Feature flag 'my-flag-key' is "..tostring(value).." for this user context")
```

Now that your application is ready, run the application to see what value we get.
```lua
lua hello.lua
```

You should see:
`Feature flag my-flag-key is FALSE for this context`