# Third-party notices

axx is licensed under the Apache License, Version 2.0 (see `LICENSE`). It includes the
third-party material listed below, which remains under its own license.

## @cucumber/html-formatter 24.1.1

- Source: https://github.com/cucumber/html-formatter (npm package `@cucumber/html-formatter`,
  version 24.1.1)
- License: MIT
- Used by: the `html` reporter (`internal/report/html.go`)
- Vendored files, embedded into the axx binary, in `internal/report/html/`:
  - `index.mustache`, `icon.url`: copied unmodified from `dist/src/`
  - `main.css`: copied unmodified from `dist/`
  - `main.js.gz`: `dist/main.js`, unmodified but gzip-compressed (`gzip -9 -n`);
    SHA-256 of the uncompressed file:
    `91063fd520049873df51ad63a278dd0ca7e8b3e36354e79cb9822ecca47b0017`
  - `LICENSE`: the package license, reproduced below

```text
MIT License

Copyright (c) 2022 SmartBear Software and contributors

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

`main.js` is a webpack bundle of the formatter's React UI (`@cucumber/react-components` and
its dependencies). The bundle's own license notices (`dist/main.js.LICENSE.txt` in the
package) are reproduced verbatim:

```text
/*! *****************************************************************************
Copyright (C) Microsoft. All rights reserved.
Licensed under the Apache License, Version 2.0 (the "License"); you may not use
this file except in compliance with the License. You may obtain a copy of the
License at http://www.apache.org/licenses/LICENSE-2.0

THIS CODE IS PROVIDED ON AN *AS IS* BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
KIND, EITHER EXPRESS OR IMPLIED, INCLUDING WITHOUT LIMITATION ANY IMPLIED
WARRANTIES OR CONDITIONS OF TITLE, FITNESS FOR A PARTICULAR PURPOSE,
MERCHANTABLITY OR NON-INFRINGEMENT.

See the Apache Version 2.0 License for specific language governing permissions
and limitations under the License.
***************************************************************************** */

/**
 * @license React
 * react-dom-client.production.js
 *
 * Copyright (c) Meta Platforms, Inc. and affiliates.
 *
 * This source code is licensed under the MIT license found in the
 * LICENSE file in the root directory of this source tree.
 */

/**
 * @license React
 * react-dom.production.js
 *
 * Copyright (c) Meta Platforms, Inc. and affiliates.
 *
 * This source code is licensed under the MIT license found in the
 * LICENSE file in the root directory of this source tree.
 */

/**
 * @license React
 * react-jsx-runtime.production.js
 *
 * Copyright (c) Meta Platforms, Inc. and affiliates.
 *
 * This source code is licensed under the MIT license found in the
 * LICENSE file in the root directory of this source tree.
 */

/**
 * @license React
 * react.production.js
 *
 * Copyright (c) Meta Platforms, Inc. and affiliates.
 *
 * This source code is licensed under the MIT license found in the
 * LICENSE file in the root directory of this source tree.
 */

/**
 * @license React
 * scheduler.production.js
 *
 * Copyright (c) Meta Platforms, Inc. and affiliates.
 *
 * This source code is licensed under the MIT license found in the
 * LICENSE file in the root directory of this source tree.
 */
```

## OpenH264 (Cisco Systems, Inc.)

OpenH264 Video Codec provided by Cisco Systems, Inc.

- Source: https://www.openh264.org; Cisco's builds of release 2.6.0, from
  https://ciscobinary.openh264.org
- Used by: the web, desktop and mobile packs' videos (`packs/internal/video`), which it encodes as
  H.264
- Not included in axx: the first time a scenario keeps a video, axx downloads Cisco's build for
  the machine's OS from Cisco, checks it against its SHA-256, and keeps it in axx's cache.
  Each pack's `videos` setting (`packs.web-core.videos`, `packs.desktop-core.videos`,
  `packs.mobile-core.videos`) turns its use on (`failed`, `always`) and off (`never`, the
  default); with videos off, axx downloads and uses nothing of it.
- Cisco's terms for its build, reproduced as they ask:

```text
-------------------------------------------------------
About The Cisco-Provided Binary of OpenH264 Video Codec
-------------------------------------------------------

Cisco provides this program under the terms of the BSD license.  

Additionally, this binary is licensed under Cisco’s AVC/H.264 Patent Portfolio License from MPEG LA, at no cost to you, provided that the requirements and conditions shown below in the AVC/H.264 Patent Portfolio sections are met.  

As with all AVC/H.264 codecs, you may also obtain your own patent license from MPEG LA or from the individual patent owners, or proceed at your own risk.  Your rights from Cisco under the BSD license are not affected by this choice.  

For more information on the OpenH264 binary licensing, please see the OpenH264 FAQ found at http://www.openh264.org/faq.html#binary 

A corresponding source code to this binary program is available under the same BSD terms, which can be found at http://www.openh264.org

-----------
BSD License
-----------

Copyright © 2014 Cisco Systems, Inc.

All rights reserved.

Redistribution and use in source and binary forms, with or without modification, are permitted provided that the following conditions are met:

1. Redistributions of source code must retain the above copyright notice, this list of conditions and the following disclaimer.

2. Redistributions in binary form must reproduce the above copyright notice, this list of conditions and the following disclaimer in the documentation and/or other materials provided with the distribution.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS “AS IS” AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE DISCLAIMED.  IN NO EVENT SHALL THE COPYRIGHT HOLDER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.

-----------------------------------------
AVC/H.264 Patent Portfolio License Notice
-----------------------------------------

The binary form of this Software is distributed by Cisco under the AVC/H.264 Patent Portfolio License from MPEG LA, and is subject to the following requirements, which may or may not be applicable to your use of this software: 

THIS PRODUCT IS LICENSED UNDER THE AVC PATENT PORTFOLIO LICENSE FOR THE PERSONAL USE OF A CONSUMER OR OTHER USES IN WHICH IT DOES NOT RECEIVE REMUNERATION TO (i) ENCODE VIDEO IN COMPLIANCE WITH THE AVC STANDARD (“AVC VIDEO”) AND/OR (ii) DECODE AVC VIDEO THAT WAS ENCODED BY A CONSUMER ENGAGED IN A PERSONAL ACTIVITY AND/OR WAS OBTAINED FROM A VIDEO PROVIDER LICENSED TO PROVIDE AVC VIDEO.  NO LICENSE IS GRANTED OR SHALL BE IMPLIED FOR ANY OTHER USE.  ADDITIONAL INFORMATION MAY BE OBTAINED FROM MPEG LA, L.L.C. SEE HTTP://WWW.MPEGLA.COM

Accordingly, please be advised that content providers and broadcasters using AVC/H.264 in their service may be required to obtain a separate use license from MPEG LA, referred to as "(b) sublicenses" in the SUMMARY OF AVC/H.264 LICENSE TERMS from MPEG LA found at http://www.openh264.org/mpegla

---------------------------------------------
AVC/H.264 Patent Portfolio License Conditions
---------------------------------------------

In addition, the Cisco-provided binary of this Software is licensed under Cisco's license from MPEG LA only if the following conditions are met:

1. The Cisco-provided binary is separately downloaded to an end user’s device, and not integrated into or combined with third party software prior to being downloaded to the end user’s device;

2. The end user must have the ability to control (e.g., to enable, disable, or re-enable) the use of the Cisco-provided binary;

3. Third party software, in the location where end users can control the use of the Cisco-provided binary, must display the following text:

       "OpenH264 Video Codec provided by Cisco Systems, Inc."

4.  Any third-party software that makes use of the Cisco-provided binary must reproduce all of the above text, as well as this last condition, in the EULA and/or in another location where licensing information is to be presented to the end user.  
 


                          v1.0
```

## OWASP Application Security Verification Standard 5.0.0

- Source: https://github.com/OWASP/ASVS, at the commit of its 5.0.0 release
  (`5cf9b032440be53ce345ab3c130fda46ba1ce7a2`)
- License: Creative Commons Attribution-ShareAlike 4.0 International
  (https://creativecommons.org/licenses/by-sa/4.0/). Copyright © 2008-2025 The OWASP Foundation.
- Used by: the `controls-asvs` pack (`packs/controls/asvs`), which shows a requirement's text
  with its source. It is in a project's axx only when the project adds that pack.
- Vendored file, embedded into the pack, in `packs/controls/asvs/data/`:
  - `asvs.flat.json.gz`:
    `5.0/docs_en/OWASP_Application_Security_Verification_Standard_5.0.0_en.flat.json`,
    unmodified but gzip-compressed; SHA-256 of the uncompressed file:
    `8201b20eec2908c3380ac600c91c8ba746346fbb808859366abb232027532311`
  - `LICENSE.md`: the notice above, beside the file
- The license covers the standard, not axx: axx reads the file, and what it writes of a
  project's controls (its evidence, its reports' data) names requirements by their IDs and has
  none of the standard's text.

## NIST SP 800-53, revision 5 (release 5.2.0)

- Source: https://github.com/usnistgov/oscal-content, at commit
  `5006f18c8047643eaa70e35dd0d20a58bcc41856`:
  `nist.gov/SP800-53/rev5/json/NIST_SP-800-53_rev5_catalog-min.json` (SHA-256
  `1cbaf5f25aa1c70610324e278323f1d3be380b9183a0e1f44074976d5da55291`)
- License: in the public domain, as a work of the United States government (CC0 1.0 worldwide)
- Used by: the `controls-nist` pack (`packs/controls/nist`)
- Derived file, embedded into the pack, in `packs/controls/nist/data/`:
  - `sp800-53.json.gz`: each control's and enhancement's ID, title and family, taken from the
    catalog by `internal/tools/controlsdata`, gzip-compressed

## NIST OSCAL, the schemas of its assessment results (1.1.3 and 1.2.3)

- Source: https://github.com/usnistgov/OSCAL, the releases `v1.1.3` and `v1.2.3`:
  `oscal_assessment-results_schema.json` (SHA-256
  `d9e34757f0c12aff61f52b821f0b8f83ba0ba75b3a149a202b08ba82f82bc4c3` and
  `4034e2032332dbf597e59e0646ec16c2c31df992962490371e91f47b219ff42c`)
- License: in the public domain, as a work of the United States government (CC0 1.0 worldwide)
- Used by: the tests of the `controls-oscal` pack (`packs/controls/oscal`), which hold the
  documents it writes to them. They are in no build of axx.
- Unmodified files, gzip-compressed, in `packs/controls/oscal/testdata/`:
  - `oscal_assessment-results_schema-1.1.3.json.gz`
  - `oscal_assessment-results_schema-1.2.3.json.gz`

