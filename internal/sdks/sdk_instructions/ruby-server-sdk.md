# Installation steps
1. Create a new directory for your application:
```shell
mkdir hello-ruby && cd hello-ruby && bundle init
```
2. Next, install the SDK:
```shell
echo "gem 'launchdarkly-server-sdk', '8.4.1'" >> Gemfile && bundle install
```
3. Create a file called `main.rb` and add the following code:
```ruby
require 'ldclient-rb'

# Set sdk_key to your LaunchDarkly SDK key before running
sdk_key = ENV['LAUNCHDARKLY_SDK_KEY']

# Set feature_flag_key to the feature flag key you want to evaluate
feature_flag_key = 'my-flag-key'

if sdk_key == ''
  puts "*** Please set the LAUNCHDARKLY_SDK_KEY environment variable\n"
  exit 1
elsif feature_flag_key == ''
  puts "*** Please set the LAUNCHDARKLY_FLAG_KEY environment variable\n"
  exit 1
end

def show_flag_message(flag_key, flag_value)
  puts "*** The '#{flag_key}' feature flag evaluates to #{flag_value}.\n"

  if flag_value
    puts
    puts "        ██       "
    puts "          ██     "
    puts "      ████████   "
    puts "         ███████ "
    puts "██ LAUNCHDARKLY █"
    puts "         ███████ "
    puts "      ████████   "
    puts "          ██     "
    puts "        ██       "
    puts
  end
end

class FlagChangeListener
  def update(changed)
    show_flag_message(changed.key, changed.new_value)
  end
end

client = LaunchDarkly::LDClient.new(sdk_key)

if client.initialized?
  puts "*** SDK successfully initialized!\n"
else
  puts "*** SDK failed to initialize\n"
  exit 1
end

# Set up the context properties. This context should appear on your LaunchDarkly contexts dashboard
# soon after you run the demo.
context = LaunchDarkly::LDContext.create({
                                           key: 'example-user-key',
                                           kind: 'user',
                                           name: 'Sandy'
                                         })

flag_value = client.variation(feature_flag_key, context, false)

show_flag_message(feature_flag_key, flag_value)

client.flag_tracker.add_flag_value_change_listener(feature_flag_key, context, FlagChangeListener.new)

# Run the Hello App continuously to react to flag change in LaunchDarkly
thr = Thread.new {
  puts "*** Waiting for changes."
  sleep
}
thr.join
```

Now that your application is ready, run the application to see what value we get.
```shell
bundle exec env LAUNCHDARKLY_SDK_KEY=YOUR_SDK_KEY ruby main.rb
```

You should see:

`Feature flag my-flag-key is FALSE for this context`
