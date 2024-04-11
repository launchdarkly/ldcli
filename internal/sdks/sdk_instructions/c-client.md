# Installation steps
If you want to skip ahead, the final code is available in our [GitHub repository](https://github.com/launchdarkly/cpp-sdks/tree/main/examples/hello-cpp-client).

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
#include <launchdarkly/client_side/client.hpp>
#include <launchdarkly/context_builder.hpp>

#include <cstring>
#include <iostream>

// Set INIT_TIMEOUT_MILLISECONDS to the amount of time you will wait for
// the client to become initialized.
#define INIT_TIMEOUT_MILLISECONDS 3000

using namespace launchdarkly;
using namespace launchdarkly::client_side;

int main() {

    auto config = ConfigBuilder("mobile-key-from-launch-darkly-website").Build();
    if (!config) {
        std::cout << "error: config is invalid: " << config.error() << std::endl;
        return 1;
    }

    auto context =
        ContextBuilder().Kind("user", "example-user-key").Name("Sandy").Build();

    auto client = Client(std::move(*config), std::move(context));

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

    bool const flag_value = client.BoolVariation("my-flag-key", false);

    std::cout << "*** Feature flag 'my-flag-key' is "
              << (flag_value ? "true" : "false") << " for this user" << std::endl;

    return 0;
}
```

5. Create a `CMakeLists.txt` file and the following content:
```txt
cmake_minimum_required(VERSION 3.19)

project(
  CPPClientQuickstart
  VERSION 0.1
  DESCRIPTION "LaunchDarkly CPP Client-side SDK Quickstart"
  LANGUAGES CXX
)

set(THREADS_PREFER_PTHREAD_FLAG ON)
find_package(Threads REQUIRED)

add_subdirectory(cpp-sdks)

add_executable(cpp-client-quickstart main.cpp)

target_link_libraries(cpp-client-quickstart
      PRIVATE
        launchdarkly::client
        Threads::Threads
)
```

6. Create a build directory:
```shell
mkdir build && cd build
```

7. Configure the SDK with your chosen build system. Examples:
**Make**
```shell
cmake -G"Unix Makefiles" -DBUILD_TESTING=OFF ..
```
**Ninja**
```shell
cmake -G"Ninja" -DBUILD_TESTING=OFF ..
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
./cpp-client-quickstart
```

You should see:
`Feature flag my-flag-key is false`