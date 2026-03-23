# IMPORTANT - READ THIS FIRST BEFORE WORKING ON TODO ITEMS
Do ONLY ONE task at a time
Once you think you've completed the item, compile and test the feature/fix in the browser on both desktop and mobile - pay attention to spacing especially on mobile
**Browser testing means exercising the feature end-to-end, not just checking the page loads.** For a new service: actually click Install, wait for the install stream to complete, confirm the service appears with "running" status, and confirm any vhost (e.g. rustfs.test) is reachable and functional. Verify health checks pass (status must show "running", not "warning"). Do NOT consider a feature done just because the UI renders without errors.
The last step in completing any TODO item is to modify the README with any new features (if relevant) or update any sections that need updating (if necessary)
Once an item is completed move it to the "Completed:" section and tag a date/time to it
Add a TODO item to the end of the backlog if the item you just completed should be screenshotted (ie. added to the screenshot script) for the readme
Clean up any leftover screenshots from playwright/puppeteer sessions
Commit all files to the repo BUT DO NOT PUSH

# Backlog

- double check we can do everything with https://github.com/coollabsio/maxio and then move to it instead of RustFS
- create a demo mode with dummy data that does not save anything to sqlite on disk (it can create it in memory if needed) and mock anything else that would be needed to see the whole dashboard and all of its features.  Ideas for things to mock: sample sites with various settings and frameworks, sample dumps from the sample sites, sample mail from the various sites, sample spx profiles from the various sites.  All the services enabled with different statuses shown.  Etc.
- an auto updater that updates from github's latest release binary

# Completed

- add whodb service - https://docs.whodb.com/installation#download-pre-compiled-binaries - download the binary don't do docker.  Make sure we preconfigure values for postgres, mysql, redis, etc. if any of those services are installed.  Also need to have some sort of hook system where if another service is installed/uninstalled it updates whodb.  I think we can generalize the hook system so we can reuse it with other services to hook into things.  Also need to add the ability to configure whodb manually - maybe we do it via a custom settings ui.  Also when we install we need to enable a sidebar item too that iframes to the whodb service.  Maybe we can generalize sidebar hooks too so that when services are installed it can add items to the sidebar easily. *(completed 2026-03-20)*
- add rustfs as a service, always download latest binary https://rustfs.com/download/ and configure vhost (like meilisearch) - https://docs.rustfs.com/integration/nginx.html and add a config setup https://docs.rustfs.com/installation/linux/single-node-single-disk.html#_5-configure-environment-variables  DO NOT install it as a systemd service, DO NOT install it via apt *(completed 2026-03-20)*
- poll the https://dl.static-php.dev/static-php-cli/bulk/?format=json api equivalent for the github binaries we build using actions for patch versions of each major version of PHP installed and show a browser notification if there is a newer version available, also add an update button to the services.  In fact, we should probably implement an "updater" system to each service.  We can look at the docs of each service to note if there are any migration steps and perform those as well.  For instance, Meilisearch I know needs to dump the data install the new version and import the dump.  PHP we can just stop the service, replace the binaries, etc.  So by the end, I want each service to have an "updater" sub system that knows how each service individually needs to be updated.  Each service should also have a check for updates method, and each service should show an update button with a tooltip on which version you are updating from/to. *(completed 2026-03-20)*
- update readme with browser notification functionality *(completed 2026-03-19)*
- add spx profile functionality similar to dumps or mail - fully integrated *(completed 2026-03-19)*
- embed speedscope flamegraph viewer into SPX Profiler — replaces hand-rolled SVG with fully-featured WebGL flamegraph; SampledProfile JSON format; dedicated `/speedscope/` route; mobile overflow fix *(completed 2026-03-19)*
- add a logs directory and make each service write their logs there — log rotation at 10 MB with 3 backups, DNS log file created, central Logs page with live SSE streaming *(completed 2026-03-19)*
- make sure that each service has a config file if it's supported and write config changes there — Valkey uses full valkey.conf, Meilisearch uses config.toml, Typesense uses typesense.ini, Mailpit uses MP_* env vars in config.env; PHP ini seeded from php.ini-development with dev-safe OPcache (validate_timestamps=1, revalidate_freq=0); all config files written once and user-editable *(completed 2026-03-20)*
- fix mysql error in logs: mysqld: Can't open shared library component_reference_cache.so — installer now extracts plugin .so files and ICU data to lib/mysql/private/; EnsureMySQLPlugins startup migration repairs existing installs automatically *(completed 2026-03-20)*
