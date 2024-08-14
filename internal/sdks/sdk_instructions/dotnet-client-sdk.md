# Installation steps
1. Create a new folder for your project:
```shell
mkdir HelloDotNetClient
cd HelloDotNetClient
```

2. Next, create a new console application:
```shell
dotnet new console
```

3. Next, add the LaunchDarkly dependency to the project:
```shell
dotnet add package Launchdarkly.ClientSdk
```

4. Open the file `Program.cs` and add the following code:
```csharp
using LaunchDarkly.Sdk;
using LaunchDarkly.Sdk.Client;

var context = Context.New("context-key-123abc");
var timeSpan = TimeSpan.FromSeconds(10);
var client = LdClient.Init(
  Configuration.Default("myMobileKey", ConfigurationBuilder.AutoEnvAttributes.Enabled),
  context,
  timeSpan
);

if (client.Initialized)
{
    Console.WriteLine("SDK successfully initialized!");
}
else
{
    Console.WriteLine("SDK failed to initialize");
    Environment.Exit(1);
}

var flagValue = client.BoolVariation("my-flag-key", false);

Console.WriteLine(string.Format("Feature flag 'my-flag-key' is {0}", flagValue));

// Here we ensure that the SDK shuts down cleanly and has a chance to deliver analytics
// events to LaunchDarkly before the program exits. If analytics events are not delivered,
// the context properties and flag usage statistics will not appear on your dashboard. In
// a normal long-running application, the SDK would continue running and events would be
// delivered automatically in the background.
client.Dispose();
```

Now that your application is ready, run the application to see what value we get.

Use the following command to run the code:
```shell
dotnet run
```

You should see:

`Feature flag my-flag-key is FALSE for this context`
