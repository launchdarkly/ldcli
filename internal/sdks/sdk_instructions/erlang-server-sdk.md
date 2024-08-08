# Installation steps
1. Create a new project for your application:
```
rebar3 new app hello_erlang && cd hello_erlang
```

2. Next, add the SDK package to your list of dependencies in `rebar.config`:
```erlang
{ldclient, "v3.2.0", {pkg, launchdarkly_server_sdk}}
```

3. Replace the `ChildSpecs` variable in `src/hello_erlang_sup.erl` with the following:
```erlang
[{console,
            {hello_erlang_server, start_link, []},
            permanent, 5000, worker, [hello_erlang_server]}]
```

4. Edit `src/hello_erlang.app.src` to import LaunchDarkly:
```erlang
{applications,
	[kernel,
	stdlib,
	ldclient
]},
```

5. First create a new file named `src/hello_erlang_server.erl`. Then, in `src/hello_erlang_server.erl` create a new `LDClient` with your *environment-specific* SDK key:
```erlang
-module(hello_erlang_server).
-behaviour(gen_server).

-export([init/1, handle_call/3, handle_cast/2,
         handle_info/2, terminate/2, code_change/3]).

-export([start_link/0]).
-export([get/3]).

%% public functions

start_link() ->
  gen_server:start_link({local, hello_erlang_server}, ?MODULE, [], []).

get(Key, Fallback, ContextKey) -> gen_server:call(?MODULE, {get, Key, Fallback, ContextKey}).

%% gen_server callbacks

init(_Args) ->
  ldclient:start_instance("1234567890abcdef", #{
        http_options => #{
            tls_options => ldclient_config:tls_basic_options()
        }
    }),
  {ok, []}.

handle_call({get, Key, Fallback, ContextKey}, _From, State) ->
  Flag = ldclient:variation(Key, ldclient_context:new(ContextKey), Fallback),
  {reply, Flag, State}.

handle_cast(_Request, State) ->
  {noreply, State}.

handle_info(_Info, State) ->
  {noreply, State}.

terminate(_Reason, _State) ->
  ok.

code_change(_OldVsn, State, _Extra) ->
  {ok, State}.
```

Now that your application is ready, run the application to see what value we get.
```shell
rebar3 shell
```
```shell
hello_erlang_server:get(<<"my-flag-key">>, "FALLBACK_VALUE", <<"user@example.com">>).
```

You should see:
`Feature flag my-flag-key is FALSE for this context`