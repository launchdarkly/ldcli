# Installation steps
1. First, ensure the required build dependencies are installed:
- C++ 17
- CMake 3.19 or higher
- A build system such as make, Ninja, or MSVC
- Boost 1.81 or higher
- OpenSSL

2. Create a new project directory:
```shell
mkdir hello-cpp-client && cd hello-cpp-client
```

3. Clone the C++ SDK inside the directory you created above using git:
```shell
git clone https://github.com/launchdarkly/cpp-sdks.git
```

4. Create a file named `main.cpp` and add the following code:
```cpp
#include <launchdarkly/context_builder.hpp>
#include <launchdarkly/server_side/client.hpp>
#include <launchdarkly/server_side/config/config_builder.hpp>

#include <cstring>
#include <iostream>

// Set INIT_TIMEOUT_MILLISECONDS to the amount of time you will wait for
// the client to become initialized.
#define INIT_TIMEOUT_MILLISECONDS 3000

using namespace launchdarkly;
using namespace launchdarkly::server_side;

int main() {
    auto config = ConfigBuilder("1234567890abcdef").Build();
    if (!config) {
        std::cout << "error: config is invalid: " << config.error() << std::endl;
        return 1;
    }

    auto client = Client(std::move(*config));

    auto start_result = client.StartAsync();

    if (auto const status = start_result.wait_for(
            std::chrono::milliseconds(INIT_TIMEOUT_MILLISECONDS));
        status == std::future_status::ready) {
        if (start_result.get()) {
            std::cout << "*** SDK successfully initialized!" << std::endl;
        } else {
            std::cout << "*** SDK failed to initialize" << std::endl;
            return 1;
        }
    } else {
        std::cout << "*** SDK initialization didn't complete in "
                  << INIT_TIMEOUT_MILLISECONDS << "ms" << std::endl;
        return 1;
    }

    auto const context =
        ContextBuilder().Kind("user", "example-user-key").Name("Sandy").Build();

    bool const flag_value =
        client.BoolVariation(context, "my-flag-key", false);

    std::cout << "*** Feature flag 'my-flag-key' is "
              << (flag_value ? "true" : "false") << " for this user" << std::endl;

    return 0;
}
```

5. Create a `CMakeLists.txt` file and the following content:
```txt
cmake_minimum_required(VERSION 3.19)

project(
  CPPServerQuickstart
  VERSION 0.1
  DESCRIPTION "LaunchDarkly CPP Server-side SDK Quickstart"
  LANGUAGES CXX
)

set(THREADS_PREFER_PTHREAD_FLAG ON)
find_package(Threads REQUIRED)

add_subdirectory(cpp-sdks)

add_executable(cpp-server-quickstart main.cpp)

target_link_libraries(cpp-server-quickstart
      PRIVATE
        launchdarkly::server
        Threads::Threads
)
```

6. Create a build directory:
```shell
mkdir build && cd build
```

7. Configure the SDK with your chosen build system: Examples:
**Make**
```shell
cmake -G"Unix Makefiles" -DBUILD_TESTING=OFF ..
```
**Ninja**
```shell
cmake -G"Unix Makefiles" -DBUILD_TESTING=OFF ..
```
**Microsoft Visual Studio**
```shell
cmake -G"Visual Studio 17 2022" -DBUILD_TESTING=OFF ..
```

8. Build the SDK:
```shell
cmake --build .
```

Now that your application is ready, run the application to see what value we get.
```shell
./cpp-server-quickstart
```

You should see:
`Feature flag my-flag-key is false`