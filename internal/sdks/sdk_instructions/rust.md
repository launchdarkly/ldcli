# Installation steps
If you want to skip ahead, the final code is available in our [GitHub repository](https://github.com/launchdarkly/hello-rust).

1. Create a new project using [Cargo](https://doc.rust-lang.org/book/ch01-01-installation.html#installation).
```shell
cargo new hello-rust && cd hello-rust
```

2. Next, install the LaunchDarkly SDK as a dependency in your application.
```shell
cargo add launchdarkly-server-sdk
```

3. Next, open `Cargo.toml` and add the following right below `[dependencies]`.
```toml
tokio = { version = "1", features = ["rt", "macros"] }
```

4. Open the file `src/main.rs` and replace the existing code with the following code:
```rust
use launchdarkly_server_sdk::{Client, ConfigBuilder, ContextBuilder};

#[tokio::main]
async fn main() {
    let sdk_key = "1234567890abcdef";
    let feature_flag_key = "my-flag-key";

    let config = ConfigBuilder::new(&sdk_key)
        .build()
        .expect("Config failed to build");
    let client = Client::build(config).expect("Client failed to build");

    // Starts the client using the currently active runtime.
    client.start_with_default_executor();

    // Wait to ensure the client has fully initialized.
    if !client.initialized_async().await {
        panic!("SDK failed to initialize");
    }

    // Set up the context properties. This context should appear on your LaunchDarkly contexts dashboard
    // soon after you run the demo.
    let context = ContextBuilder::new("example-user-key")
        .build()
        .expect("Context failed to build");

    let result = client.bool_variation(&context, &feature_flag_key, false);
    println!(
        "Feature flag '{}' is {} for this context",
        feature_flag_key, result
    );

    // Here we ensure that the SDK shuts down cleanly and has a chance to deliver analytics events
    // to LaunchDarkly before the program exits. If analytics events are not delivered, the context
    // properties and flag usage statistics will not appear on your dashboard. In a normal
    // long-running application, the SDK would continue running and events would be delivered
    // automatically in the background.
    client.close();
}
```

Now that your application is ready, run the application to see what value we get.
```shell
cargo run
```

You should see:
`Feature flag my-flag-key is FALSE for this context`