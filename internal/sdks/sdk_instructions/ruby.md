# Installation steps
1. Create a new directory for your application:
```shell
mkdir hello-ruby && cd hello-ruby && bundle init
```
2. Next, install the SDK:
```shell
echo "gem 'launchdarkly-server-sdk', '8.4.0'" >> Gemfile && bundle install
```
3. Create a file called `main.rb` and add the following code:
```ruby
# Import the LaunchDarkly client.
require 'ldclient-rb'

# Create a new LDClient with your environment-specific SDK key.
client = LaunchDarkly::LDClient.new("1234567890abcdef")

# Create a helper function for rendering messages.
def show_message(s)
  puts "*** #{s}"
  puts
end

if client.initialized?
  show_message "SDK successfully initialized!"
else
  show_message "SDK failed to initialize"
  exit 1
end

# Set up the context properties. This context should appear on your LaunchDarkly contexts dashboard
# soon after you run the demo.
context = LaunchDarkly::LDContext.create({
  key: "example-user-key",
  kind: "user",
  name: "Sandy"
})

# Call LaunchDarkly with the feature flag key you want to evaluate.
flag_value = client.variation("my-flag-key", context, false)

show_message "Feature flag 'my-flag-key' is #{flag_value} for this context"

# Here we ensure that the SDK shuts down cleanly and has a chance to deliver analytics
# events to LaunchDarkly before the program exits. If analytics events are not delivered,
# the context properties and flag usage statistics will not appear on your dashboard. In a
# normal long-running application, the SDK would continue running and events would be
# delivered automatically in the background.
client.close()
```

Now that your application is ready, run the application to see what value we get.
```shell
bundle exec ruby main.rb
```

You should see:

`Feature flag my-flag-key is FALSE for this context`