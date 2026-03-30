# Google Tag Manager & Google Analytics Proxy
![Header](http://i.blaumedia.com/gogtmgaproxy-github-header.png)
- [Introduction](#introduction)
- [Features](#features)
- [Installation](#installation)
- [Chrome Extension](#chrome-extension)
- [Performance](#performance)
- [Plugins](#plugins)
- [To do/Known problems](#to-do)
- [Support](#support)
- [Donate](#donate)


## Introduction
The fight against tracking blockers goes into the next round! Companies depend on having a solid data base to make the right decisions and to understand their customers accurately. This proxy server makes it possible to achieve this with minimal effort.

But how exactly does a "proxy" for the Google Tag Manager and Google Analytics work? And what is a proxy actually?

![Description of how a proxy server for GTM + GA in general works.](https://user-images.githubusercontent.com/4989256/55686879-542d2f00-596f-11e9-8313-5837af75cc2e.png)
[Credits to ZitRos](https://github.com/ZitRos/save-analytics-from-content-blockers) and his proxy for this scheme!

To prevent tracking, blockers try to block the URLs of www.googletagmanager.com and www.google-analytics.com among several other sites. So this proxy server integrates itself into your website and makes https://www.googletagmanager.com/gtm.js?id=GTM-ABC123 (f.e.) to https://www.yourdomain.tld/K3ZQ8HD.js?id=ABC123. Thus a Tracking Blocker does not realize that the Google Tag Manager is behind the script. The same happens with the script of Google Analytics (https://www.google-analytics.com/analytics.js). The proxy will change the URLs in the scripts to route the traffic through the proxy instead of requesting Google servers. Additionally the proxy of the Google Analytics Javascript has a *built-in solution* for the client-side cookie problematic that came up with Apples Intelligent Tracking Prevention (ITP) and some other tweaks.

## Features
- Bypass tracking blockers to load your Google Tag Manager
- Bypass tracking blockers to load the Google Analytics script and send pageviews/events/... to Analytics
- Adjustable cache for the above scripts to increase performance (see [Performance](#performance) section to see the benefits and comparison to [ZitRos proxy solution](https://github.com/ZitRos/save-analytics-from-content-blockers))
- Use of server-side cookie to keep the google client id in users browsers to up to 2 years (ITP 2.3 bypass)
- Chrome Extension for the trouble-free use of the Google Tag Manager preview mode (see [Chrome Extension](#chrome-extension) for the why)
- High performant and full RFC compliant thanks to Go and [net/http](https://golang.org/pkg/net/http/)
- JS minifier for optimizations of up to 30% (thanks to [tdewolff/minify](https://github.com/tdewolff/minify) and [mishoo/UglifyJS](https://github.com/mishoo/UglifyJS))
- Small ready-2-use [docker image on GHCR](https://ghcr.io/dnnspaul/go-gtm-ga-proxy) (`linux/amd64`) for fast deployments and close-to-zero disk usage
- Many environment variables to adjust the proxy exactly to your needs
- Expandable through [Plugins](#plugins)

## Installation
First of all, you'll need an installed docker daemon on your server. See the [docker docs](https://docs.docker.com/install/) for more information.

There are many possible solutions to integrate the proxy into your system. Please read the documentation of *each environment variable* carefully.
Starting the proxy is possible with following command and environment variables:
```shell
docker run \
    -e ENDPOINT_URI=www.yourdomain.tld \
    -e JS_SUBDIRECTORY=js \
    -e GA_CACHE_TIME=3600 \
    -e GTM_CACHE_TIME=3600 \
    -e GTM_FILENAME=gtm.js \
    -e GTM_A_FILENAME=gtm_a.js \
    -e GA_FILENAME=ga.js \
    -e GADEBUG_FILENAME=ga_debug.js \
    -e GA_PLUGINS_DIRECTORYNAME=links \
    -e GTAG_FILENAME=tag.js \
    -e RESTRICT_GTM_IDS=false \
    -e GTM_IDS=YOUR_GTM_ID_WITHOUT_GTM- \
    -e GA_COLLECT_ENDPOINT=/fetch \
    -e GA_COLLECT_REDIRECT_ENDPOINT=/fetch_r \
    -e GA_COLLECT_J_ENDPOINT=/fetch_j \
    -e GA4_COLLECT_ENDPOINT=/g/fetch \
    -e PROXY_IP_HEADER=X-Forwarded-For \
    -e PROXY_IP_HEADER_INDEX=0 \
    -e ENABLE_SERVER_SIDE_GA_COOKIES=true \
    -e GA_SERVER_SIDE_COOKIE=_gggp \
    -e GA_CLIENT_SIDE_COOKIE=_ga \
    -e COOKIE_DOMAIN=yourdomain.tld \
    -e COOKIE_SECURE=true \
    -p "8080:8080" \
    ghcr.io/dnnspaul/go-gtm-ga-proxy:latest
```


|Variable|Description|Example|
|-----------------|-----|-----|
|```ENABLE_DEBUG_OUTPUT```|Set this to true to let the proxy print debug details for setting up or debugging.|false|
|```ENDPOINT_URI```|URI where the proxy will be reachable at. If not present, will use `Host` header.|www.google.com|
|```JS_SUBDIRECTORY```|It is intended to serve the javascript files within a subdirectory of the URI. Here you can define a name for it.|js|
|```HTML_SUBDIRECTORY```|It is intended to serve the html files (like the GTM iframe) within a subdirectory of the URI. Here you can define a name for it.|html|
|```GA_CACHE_TIME```|Time in seconds the proxy caches the Google Analytics client javascript.|3600|
|```GTM_CACHE_TIME```|Time in seconds the proxy caches the Google Tag Manager client javascript.|3600|
|```GTM_FILENAME```|The filename the GTM javascript file is reachable at.|gtm_inject.js|
|```GTM_A_FILENAME```|The alternative filename the GTM javascript file is reachable at. (www.googletagmanager.com/a)|container_a.js|
|```GA_FILENAME```|The filename the Google Analytics javascript file is reachable at.|ga_inject.js|
|```GADEBUG_FILENAME```|The filename the [Google Analytics debug](https://developers.google.com/analytics/devguides/collection/analyticsjs/debugging) javascript file is reachable.|gadebug_inject.js|
|```GA_PLUGINS_DIRECTORYNAME```|The directory name where google analytics plugins will be accessable at.|links|
|```GTAG_FILENAME```|The filename where gtag will be accessable at.|tag.js|
|```RESTRICT_GTM_IDS```|Set to true if you want to enable a whitelist for the GTM-IDs.|false|
|```GTM_IDS```|Here you can setup the whitelist for GTM ids. Comma-separate if you want to add multiple ids. Just put the ids without the leading 'GTM-'.|NNQJ5LT,N5ZZT3|
|```GA_COLLECT_ENDPOINT```|Set the new name for the /collect endpoint.|/fetch|
|```GA_COLLECT_REDIRECT_ENDPOINT```|Set the new name for the /r/collect endpoint.|/fetch_r|
|```GA_COLLECT_J_ENDPOINT```|Set the new name for the /j/collect endpoint. Couldn't find out yet for what this endpoint is. If you know more, please share! :)|/fetch_j|
|```GA4_COLLECT_ENDPOINT```|Set the obfuscated path for the GA4 Measurement Protocol `/g/collect` endpoint. Required for GA4 data collection to be proxied.|/g/fetch|
|```PROXY_IP_HEADER```|The header variable where the proxy will find the IP address of the user.|X-Forwarded-For|
|```PROXY_IP_HEADER_INDEX```|The header variable value will be split by comma. With this variable you can set the index of the users IP.|0|
|```ENABLE_SERVER_SIDE_GA_COOKIES```|If the proxy should transfer the client id to a serverside cookie, set this to true.|true|
|```GA_SERVER_SIDE_COOKIE```|Set the cookie name for the serverside cookie.|_gggp|
|```GA_CLIENT_SIDE_COOKIE```|Set the cookie name for the clientside cookie, where google analytics will take the client id from.|_ga|
|```COOKIE_DOMAIN```|The [domain](https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/Set-Cookie#Attributes) where the cookies are setup to. If not present, use the `Host` header.|google.com|
|```COOKIE_SECURE```|If your website is accessable through https://, you should set this variable to true. [More info](https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/Set-Cookie#Secure)|true|


There are two ways to integrate the proxy into your existing system. You have the choice between integration using a subdomain or a subdirectory on your website. When integrating through a subdomain, make sure that you place the cookies on the root domain and not under the subdomain; this can be configured with the environment variable ```COOKIE_DOMAIN```. Another important setting that must be configured in the frontend proxy is the deactivation of rewrite of forwardings. The Google Measurement Protocol will return a 302 redirect on /r/collect requests, leading to a Google Ads endpoint. In order for the end user to be redirected there, the redirect rewrite must be disabled. Example configs for nginx and apache2 are below. If you successfully integrated the proxy into another solution, feel free to fork, edit the docs & submit a pull request! :)

### Integration in JavaScript/Frontend
Change the GTM integration snippet from:

```javascript
<!-- Google Tag Manager -->
<script>(function(w,d,s,l,i){w[l]=w[l]||[];w[l].push({'gtm.start':
new Date().getTime(),event:'gtm.js'});var f=d.getElementsByTagName(s)[0],
j=d.createElement(s),dl=l!='dataLayer'?'&l='+l:'';j.async=true;j.src=
'https://www.googletagmanager.com/gtm.js?id='+i+dl;f.parentNode.insertBefore(j,f);
})(window,document,'script','dataLayer','GTM-XXXX');</script>
<!-- End Google Tag Manager -->
```

to where your proxy is reachable at:

```javascript
<!-- Google Tag Manager -->
<script>(function(w,d,s,l,i){w[l]=w[l]||[];w[l].push({'gtm.start':
new Date().getTime(),event:'gtm.js'});var f=d.getElementsByTagName(s)[0],
j=d.createElement(s),dl=l!='dataLayer'?'&l='+l:'';j.async=true;j.src=
'https://gggp.example.com/js/gtm.js?id='+i+dl;f.parentNode.insertBefore(j,f);
})(window,document,'script','dataLayer','XXXX');</script>
<!-- End Google Tag Manager -->
```

### Example integration using subdirectory
Please note that 127.0.0.1 is an example IP in the following configs. Obviously you have to change it to the ip address where the proxy is reachable.

#### nginx
```nginx
[..]

location /goproxy/ {
    proxy_set_header  X-Forwarded-For $remote_addr;
    proxy_set_header  X-Forwarded-Host $remote_addr;

    proxy_pass http://127.0.0.1:8080/;
    proxy_redirect off;
}

[..]
```

#### apache2
Put the following into the httpd.conf and not into the virtualhosts-files.
```apache2
[..]

ProxyPass "/goproxy" "http://127.0.0.1:8080"

[..]
```

### Example integration using subdomain
Please note that 127.0.0.1 is an example IP in the following configs. Obviously you have to change it to the ip address where the proxy is reachable.

#### nginx
This vhost configuration for nginx does require the following:
```bash
apt-get update && apt-get install openssl certbot

openssl dhparam -out /etc/nginx/dhparam.pem 4096
certbot certonly --standalone -d gggp.example.com
```

```nginx
server {
        listen 443 ssl http2;
        listen [::]:443 ssl http2;

        ssl_certificate /etc/letsencrypt/live/gggp.example.com/fullchain.pem;
        ssl_certificate_key /etc/letsencrypt/live/gggp.example.com/privkey.pem;

        ssl_protocols TLSv1.2;
        ssl_dhparam /etc/nginx/dhparam.pem;
        ssl_ciphers EECDH+AESGCM:EDH+AESGCM;
        ssl_prefer_server_ciphers on;

        ssl_ecdh_curve secp384r1;

        add_header X-Content-Type-Options nosniff;
        add_header X-XSS-Protection "1; mode=block";
        add_header Strict-Transport-Security "max-age=31536000; includeSubDomains; preload";

        ssl_stapling on;
        ssl_trusted_certificate /etc/letsencrypt/live/gggp.example.com/chain.pem;
        ssl_stapling_verify on;

        resolver 8.8.8.8;

        server_name gggp.example.com;

        location / {
                proxy_set_header  X-Forwarded-For $proxy_add_x_forwarded_for;
                proxy_set_header  X-Forwarded-Host $proxy_add_x_forwarded_for;

                proxy_pass http://127.0.0.1:8080/;
                proxy_redirect off;
        }
}

server {
        listen 80;
        listen [::]:80;

        server_name gggp.example.com;

        rewrite     ^   https://$server_name$request_uri? permanent;
}
```

### apache2
```apache2
<VirtualHost *:*>
    ProxyPreserveHost On

    ProxyPass / http://127.0.0.1:8080/
    ProxyPassReverse / http://127.0.0.1:8080/

    ServerName gggp.example.com
</VirtualHost>
```

## Chrome Extension
By activating the server-side cookies you may have noticed that the preview mode of the Google Tag Manager does not work anymore. This is because as a web page visitor, you no longer send cookies directly to the Google Tag Manager domain, but instead they are sent to the proxy. However, when you click the "Preview" button in the Google Tag Manager interface, multiple cookies are set on the Google Tag Manager domain.

To solve this problem, I have created a small extension for Google Chrome that builds a bridge between the proxy and the cookies of the GTM domain. The source code is open-source and publicly available through this GitHub repository in the folder "chrome-extension".

The plugin synchronizes cookies between the googletagmanager.com domain and domains you define. For example, if you make the proxy reachable at proxy.example.com or example.com/proxy, you would need to define example.com as the domain - assuming this is the page the user is visiting.

In addition, there is also the possibility to synchronize only the cookies from specific GTM containers to specific domains. This might be especially relevant for agencies who do not want to leak preview cookies from other customers.

To install the extension you can either clone the repo & import the directory extension directly into chrome or you install the extension from the Chrome Web Store. The source is 100% the same as in this repository.

**[Open in Chrome Web Store.](https://chrome.google.com/webstore/detail/gtm-cookie-sync/jeahakippiaaenjaagcmnmpecaaphcka)**

## Performance
I would like to clarify various questions:

- Does the proxy make my website slower?
- Is the server scalable?
- Is this proxy server faster/slower than the one from Zitros?


### Does the proxy make my website slower?
Maybe, but the difference should hardly be measurable. The main factor is the performance of the server you are using. Because the Google servers use HTTP3/QUIC, we will hardly be able to reach the delivery times as long as QUIC is not standardized and implemented in the common web servers.
The proxy can automatically minimize the JS files and thus reduce the file size by up to 30%. As soon as QUIC is released and runs stable, it will of course be implemented in the proxy.

### Is the server scalable?
You can start as many of the docker containers as you like and place a simple load balancer in front of them. Container orchestrations like Kubernetes can make the setup much easier. The containers run independently and each creates its own cache.

### Is this proxy server faster/slower than the one from Zitros?
The fact that the proxy server of Zitros was developed in nodeJS gives the GoGtmGaProxy an amazing benefit in terms of speed and CPU/RAM usage due to the base in Go. NodeJS for example is not multi-core capable - so it is not suitable for sites with a lot of traffic, as we will see below. Before we get to the numbers, it should be mentioned that Zitro's proxy server simply passes the traffic through and searches & replaces text. The GoGtmGaProxy also has the server-side cookies (set at analytics.js), the JS-Minifier and the possibility to be extended with plugins.

For testing I used [siege](https://linux.die.net/man/1/siege); a website stress tool for the command line. Siege was run on a Hetzner server (8C/64GB RAM) and the proxies ran on a Google Compute Engine instance (n1-standard-2). In front of the proxies was a Nginx reverse proxy installed onto the GCE instance.

*I'm aware that this is not a reliable test, but it should show the tendencies of the performance and that's all we want to do at this point.*

I measured with
```bash
siege -c 100 --benchmark --time=1m URL
```
This means that within one minute with 100 users we try to access the website as often as possible. Important are the metrics Transactions, Availability, Longest Transaction and Shortest Transaction.

To see the results please go to the [wiki](https://github.com/blaumedia/go-gtm-ga-proxy/wiki/Performance-vs.-Zitros-nodeJS-Proxy).
## Plugins
To do.

## To Do
- We have noticed that our remarketing lists in Google Ads are no longer filled correctly, although we forward the 302 forwarding from the ad server to the user. We are investigating the problem and are grateful for every tip.

## Support
Since I use the proxy on various corporate sites with a lot of traffic, I have a great interest in a bug-free experience. So if you notice a bug or error, feel free to create an issue or pull request. If you have any questions or problems regarding the proxy, you are welcome to create an issue and I will get back to you as soon as possible.

## Donate
If you found this repo useful, please consider a donation. Thank You!

[PayPal](https://www.paypal.com/cgi-bin/webscr?cmd=_s-xclick&hosted_button_id=7K2542NW63TWQ&source=url)

[Bitcoin: 131AUMEDiAFHakkLHnvhYgvunKR6d38kZx](https://www.blockchain.com/btc/address/131AUMEDiAFHakkLHnvhYgvunKR6d38kZx)

[Ethereum: 0x9eB04Daf33DEF7dec2f5D454D1c63c020aBe8D8f](https://www.blockchain.com/eth/address/0x9eB04Daf33DEF7dec2f5D454D1c63c020aBe8D8f)
