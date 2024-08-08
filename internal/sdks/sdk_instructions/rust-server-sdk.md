# Installation steps
1. Create a new project using [Cargo](https://doc.rust-lang.org/book/ch01-01-installation.html#installation).
```shell
cargo new hello-rust && cd hello-rust
```

2. Next, install the LaunchDarkly SDK as a dependency in your application.
```shell
cargo add launchdarkly-server-sdk
```

3. Next, add `tokio` as another dependency alongside the SDK.
```shell
cargo add tokio@1 -F rt,macros
```

4. Open the file `src/main.rs` and replace the existing code with the following code:
```rust
use std::{thread, time};

use launchdarkly_server_sdk::{Client, ConfigBuilder, ContextBuilder};

fn show_evaluation_result(feature_flag_key: &str, result: bool) {
    println!(
        "*** The {} feature flag evaluates to {}",
        feature_flag_key,
        result,
    );
}

fn print_banner() {
    println!("                 ");
    println!("        ██       ");
    println!("          ██     ");
    println!("      ████████   ");
    println!("         ███████ ");
    println!("██ LAUNCHDARKLY █");
    println!("         ███████ ");
    println!("      ████████   ");
    println!("          ██     ");
    println!("        ██       ");
    println!("                 ");
}

#[tokio::main]
async fn main() {
    // Set sdk_key to your LaunchDarkly SDK key.
    let sdk_key = std::env::var("LAUNCHDARKLY_SDK_KEY")
        .expect("LAUNCHDARKLY_SDK_KEY env should be set");
    // Set feature_flag_key to the feature flag key you want to evaluate.
    let feature_flag_key = "my-flag-key";

    let config = ConfigBuilder::new(&sdk_key)
        .build()
        .expect("Config failed to build");
        let client = Client::build(config).expect("Client failed to build");
        // Starts the client using the currently active runtime.
        client.start_with_default_executor();

    // Wait to ensure the client has fully initialized.
    if !client.initialized_async().await {
        panic!("*** SDK failed to initialize. Please check your internet connection and SDK credential for any typo.");
    }

    println!("*** SDK successfully initialized.");

    // Set up the evaluation context. This context should appear on your LaunchDarkly contexts dashboard soon after you run the demo.
    let context = ContextBuilder::new("example-user-key")
        .kind("user")
        .name("Sandy")
        .build()
        .expect("Context failed to build");

    let mut show_banner = true;
    let mut last_value = None;

    loop {
        let result = client.bool_variation(&context, &feature_flag_key, false);

        if Some(result) != last_value {
            show_evaluation_result(&feature_flag_key, result);
        }

        if show_banner && result {
            print_banner();
            show_banner = false;
        }

        last_value = Some(result);
        thread::sleep(time::Duration::from_secs(1));
    }
}
```

Now that your application is ready, run the application to see what value we get.
```shell
LAUNCHDARKLY_SDK_KEY=YOUR_SDK_KEY cargo run
```

You should see:
`Feature flag my-flag-key is FALSE for this context`
