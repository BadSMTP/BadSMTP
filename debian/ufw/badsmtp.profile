[badsmtp]
title=BadSMTP test server
description=BadSMTP SMTP test server and representative delay ports
# ports: main SMTP, greeting delay ports (25200..25209), drop delay ports (25600..25609), TLS ports
ports=2525/tcp|25200:25209/tcp|25600:25609/tcp|25465/tcp|25587/tcp
