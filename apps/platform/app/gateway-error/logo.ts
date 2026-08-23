/**
 * The Octo mark, inlined.
 *
 * The error page must render with nothing else in flight: it is served for a
 * hostname that goes nowhere, and the catch-all route sends *every* path on such a
 * hostname to this page — so an <img src="/octo-logo.png"> from here would be
 * routed straight back and come back as HTML, showing a broken image. A data URI
 * is the only way this page carries the real logo rather than an invented one.
 *
 * It is public/octo-logo.png, resized to 128px (2x the 64px it renders at) and
 * palette-quantized: 1.1 MB down to about 7 KB, which is a reasonable thing to
 * inline and still the actual brand.
 *
 * Regenerate after changing the logo:
 *
 *   node -e 'const s=require("sharp");s("apps/platform/public/octo-logo.png")\
 *     .resize(128).png({palette:true,colors:256,dither:1,effort:10}).toBuffer()\
 *     .then(b=>console.log(b.toString("base64")))'
 */
export const LOGO_DATA_URI =
  "data:image/png;base64," +
  "iVBORw0KGgoAAAANSUhEUgAAAIAAAACDCAMAAABydONXAAADAFBMVEVMaXFCUfVJU/cnSHVBS/YDlvYuWJQtXLMfdcstVLUB" +
  "u+4tXaNjYv0nUKIjT5hYWfg4SfNfXfkFjO8BovZARPEtTKxITPQuQ+gFjPZAZPgGgO8vR/BpZ/pQVPdzcv8tS68zTvE1POdw" +
  "a/0mdLkDw+0lRq4Ob9UciMIsPuNEUOEidb1IVMsRbvINZu0NgM4JdfBocPktS7oqR7dkYfUrR7xjbvNWbPcBqPRIUNYyQrVM" +
  "X/YuR9QWocwOg9MJg98C7/gnQ9ZCT9AOgs9UWeBaYN5JW88KheQQttsaY/M+SNZqefw4ScxZWu4wS8VFW8c7ZOlFXcYR1ulC" +
  "RuEXjcULetUSarwLhNwsTctiZes2Td8+bvwIk+glS+kdULdEU+cqQ9gOcMkWabMTm8s6QNUVks5ddP0IlOQkSb9GUcwPd8cU" +
  "bsBWWuQKn+oxQswWa70VZtFbY9wMevAxVdRkcOthbOUKl+AtVtsTptFQZNZMUtMbsdQMbugJjtoMqOAmR7kUstcTWsYeWdEM" +
  "h9QRxd8qQccKrucKntNEY98mX+kLtuFObtu60PQTWdkMu+VDef+XvPEB7fEAwO8FcPQAs/AAz+8At+8A5PABo/AArPACgPQB" +
  "4O8wS/QA3PAAxfABn/AAmvAA1e8CiPYAu/AAr/ADe/QA2fAMaPQAl/EIbPMjUfMAqPAoTvQUXfQEdvMfVfMDhvIAyvEC6e8b" +
  "WfUC7/EBkvEBnPYEdvYCj/UPY/QCye45T/c9XvgEgvhpZv4oTvH4/v8CpfgC0fIB6PNTVPw0W/YB3vJV7elAVPsFmP4yQu0o" +
  "VfQpXPZEZf4FsPpKSvv0+v4GkftKV/5X8ewIe/MEqf0n0OUXWvBubv8Iivr///1dXf8wTvlD4+dN7epO5+Zd8uwGn/4u1+Y7" +
  "Qew43OYD8f4ExvkFuvwHwvUG0ftE7ewH3PxJju/++fdTYv7t7PUZ6u8u6+0NXt41Y/18nu8Ft/UshvEZyehWhegGVvLL0e7Z" +
  "4vKywfI5dv2exfNwgv+crukHjNmEAAAA43RSTlMA/v4I/v4PAQIE/hv+IhT+/v3+/v4s/v79/f7+/f7+Nv7+/Sf9P+xG/uI1" +
  "Y/7+nP79VmT9g/79/sNL/sSQuuv+/JGH28uK+83+1/6f/HJC9k78+lrLeNmO9+79+Pxy++SuSHvra/7mlXplke31sFfDsf6r" +
  "8dnW3KObqZX8xualr9fpqubZ/Mrc/vN1/vP8/v7//////////////////////////////////////////v//////////////" +
  "/////////////////////////////////////////////////////hhhcjAAAAAJcEhZcwAACxMAAAsTAQCanBgAABaTSURB" +
  "VHic7VpnWFTntl4wzOy9mYKUgQFGeu9VQQSRJgiIiiD2GnvvLYmamJh2rhgUBxAYRBSJIgoiOCBiA4WIgoIYI/HYYk8xOck5" +
  "95z7rG/voYx6zPFq7o/Leh5B9pT3Xe9a31rr+/YG6LVe67Ve67Ve67XXN4rusv8LcOhhNE39ifA0olO2jtMHEkt0FFIUd/XP" +
  "ghc4rhkyIqN2ELHtIz4Y8n6i8E+iQNMAtms+yLh169agLawdHnTr1qDTH6xxof4ECjQFLitHqMEPbzm8ZcuWki0lJSUlg26N" +
  "eN8RKOatwlM0CNeMYNEPE0N45FBSUrK99taIlUJ4mxrQNCR+cGvQlhIEVhM4rCZQsn177a0PHFGkt4VPUQNzB9XWotOHnzx5" +
  "oqbQKcH2jKzaEVPfGgMaBO/X1m5HqCffX7px48alpyhDNwG2b8/YnbV7DYWL8m3gC4fcqt1eW1JS8vQS2o0bN55u4ZKAxc/I" +
  "yMjavfv+22FAgYDg19aWfH/pzBmiwI3rrPscPksgN/fEwLeRiTSsuXUaUUq+v35dTeD7w0T92tqS7RkIj/hpaWm3p795BjRM" +
  "PX066zSLf/0MBuDSmRtP2fDXst5nZGQRAhUHv3B80wxocPzl9O6s0xkZ3xMCZy5dunTmzJknavG78JGA3e0hAnijaUABb8j9" +
  "3blZWVlPWXzWnmzvDs/5n1ZRsdPu7htOAxqm3t+dm7t799Pr31y//u23Z85cP3Pm+yfba9ncZ8PP+Y8EUu++a/smGVAg/OB+" +
  "bm5u7ohvvvnmm2/RCHxtN/FZfAxAhV1qqu6blYCGqfdPpKXl5n7Xif90ezftT5/Oytq9u9P/nam6unnvCt9cFlCYAWlpaWkj" +
  "/vpXjsGTDERXS4/eo/vE/wrE37bt5ze4FGlwtDvBEWAZjODiTnzvBo/62xH8L+8EvzkFaBh4rwLt8neEwbcjsjIyTmeczjqd" +
  "lXUatefUJ+4T/C937fr5zcWAAmbIPbudO3dWXP6OMPhuNyt7Z+ax8GT9peqy+Lt2pTvCGxpOaLD9ZKfdTjtCABkQn0naEd9z" +
  "c0+c4Nzfmcr5v2PHjv5h/xEB6uVy0ZCom5qaapdqd5kw+C4XK0I3dMSvUONvI/j79u2r2fjyLMQtRXdACv966TxJw9TbukhB" +
  "9xfCYAQuyNwu7LS0tIMHUX671DyCv2PfvvT0bMPglxGg2Ot0D+8Fku5XNAgMvK1LzO7y5cuXkUAaQnPoBysI/M7U1NS8PM79" +
  "9PTs7LJgTJ8X4wuHDXMRdE5OFAimvj9kyMqXdTAa1tzRzctDBr8gg4oK4jXr+cGDBysO7iTwunlq+dOzs7NzckYxLyRAgSBy" +
  "2VL/pcs+5V6nQTzk/v0Tafe+mPpiBjQMvLNt27ZtagYVOysIMGITcBaerH61+zk5e/a8mAAFgkX/HEpsFa2edBD+hN2JLxKB" +
  "oV9M4EvCIC8vz+6XXwgkS4AD5+A5fHR/z55S/osqEUUzEPnPoUP9l3oNHTr0U/SYhun3Tpz4YuVKu7R7QyTP7zpJEt758ssv" +
  "kUNe3ra81NRUQkHtOoY+D9E75c/J2VNaalA++XlBaYoC8dKhQ5cNFw9fNnToMiH5+vfvHfxiNkDiJ2kV7zvaPp+LNEzfhaWF" +
  "UNiWl4cUugwTn0XvAW9goJzzXB2gQeC9KsLfy8sVGBju7+U/nKgy5N7tj3g0Ayvv2VXYfbLyuUZOg8u8XTt2dKOAsUC/8X8E" +
  "nZQeEv2cnByELy4uNlugSYCG4R+/M/Qdf3//YcAD26VIBG3I7dsf8YCBgffs7OwO3vtEs41RIFi+a98OpKCOBOqgFp7UXYTn" +
  "3N9D8M2UDpqeUNQs/3fe6dfP339oJKeAKxuC26mbpvIk0z/ZaYd2767mamDAo3Hfvn1EBDUHVnfOd4LOJh9Rv9jMzKxtoUYO" +
  "0hCJ8MjAy3/V8E+XeXmluLC9Vjc1T/fdd3VT7e5+YmeXanf7roYGNDhm70PbQWTgWBDP2aqbzqIjPOu+2eLFiyf0jAAN3gj+" +
  "Tr/Bkf5yr1av1lYvi0VcHRh4+67u3VTd1LsrXT66q5uqe3eTSw8GFPCWN6anp6f3IKF2nfW9B7zZ4sXNDuIeClAg/Pidfv36" +
  "9RsNsMqrVe7l5WURLiYwFPAGbrp7++62d8N4IPnotm6e7u2PeqYPDdNrkABS6N8f/2G3Y8G5soOxLy2VEfjmxZ6ei+doqriK" +
  "4C+haZpxjUjxT5m7Sk2RosFlqsfARCHQDDhu+nnbtm0/J2pKsK4G3cxOTzfs379///R08oMVPmcPAS8tV+UrFM2I7rnWzYH1" +
  "rgvfMqHfgAH9JtoDQ1HAcxk2TNzVgSnurdgceMux6Nz5iNdDQBocx2XnIAPDMv48g9J0w2xDwy70cjTl4gc/PTjSvN/T083N" +
  "be1zGTC434ABAx4F4Pequ2E3huSsDxFp8GjEJNukMc7QMDsHrazs2WePf/+vZ/zssrKyMs53RJcpnn328OrDn7YivJvb5J7z" +
  "BQWSjx8hgcGcLi89W6RhaiPm2J2VGkuRhrCyPXv4Zcr/vnrh+NXHD8rL+Hw+QTcwUCqVbfkPHl+9cOH4b1fc3Aqi3dZrzIM0" +
  "DBtAzPVVozINiSTRf16usbmjgBdcVlrOV3129cKFC8cfPyjnl5eXl5aXKouLlSqV6tnj4xeOXzj+++aC6OjoDVKNIkjD4EcD" +
  "tAcMGDDs1QRc5mF679qkOR3QIFnXJCtXoQLHL1z9/RnGndTcYoVK5fkZe/m3K0ejozdYauBjBC5qa2sPcH7lpEyB7bz+uNoa" +
  "wzSXEQ2zDWQy5YOHx48j1E8KmZKgKxT5zUcePLzAXv1bdHS0HzA9P8yAt5a2trb2RedXbpopEM+owdXWso7X/TIDEgamlxfL" +
  "2vb//SoyuPqbp1JBLD9/f/PWnwiB4483f3UlerMP8Cxp9dJiCQy+qKWlpX3RmfdqAsIZhjk52dmG41y6dKQZmDIHwKPJQKlU" +
  "PHuMah//fbGKhc/P33+EJUAEuHL+/EyA9/y6TRY0SJyRgNZLCVCdp0oUMOtqcpBBjQfQDI2vMDQwsx2mgCRZWaxUFu8n+X71" +
  "s60qBM/fv//I1q17f3144cKFq3+/8tWVA+fOB/HAdkOskB2+kYdgCeIba12M4b1wLqdJBWD5MuDRgpU1JycnrJOUdDLfA2A2" +
  "CbhCsfXBbw8fPn5wBMH379+6devWgoLovz98+PhfV746dOjAuXPV3gDxpivi2YyjQBxA/NcyvhjAOkq2ARwuYF1iJEIBZhkh" +
  "MJtUtz17ygyDE215AoHtAo+QuoVC4I1qIwTym4+4Pfj1WSf+3r17CwoKjv76a8Lfjh46cKDwXPWpFUJgYhuOOcf6WAqEwyNR" +
  "fx0dHWPti5EsCAU8W5zLKdKLaEhc9+67yzeyHZAGl3FlhEBOTkfHjOTk5BADPn+yECBMiQqg6s35R44caea8R/ijaH/7Kjr6" +
  "ABI4d+7UEqB5S7TOamlNdHbWuqhN4I2NjSdasu2PHh6Bc3mkBOsy8Dwa7zQ2NjbOY6cAGjZ2EAJEhQ4+n8+PmiOgYbZZsRmB" +
  "J44T51nfC44e/Qrt0CGEL6yuLjpndcoPaCZ+zFk0HdaMjY1Prubab6SXhY2FhYVFhBj/8rjTuKOxsXFHYzqpPTTYzujg+hu/" +
  "FOGDpwADC6KUZgoFgh9RR544j96z8Cw+Eqi2srLCsVM8c4wWRwD9vxiAHuNk6G9hYyP3svCyiOABOG5q3JE+auPyHf0b15FF" +
  "QoPj8j11dWVlfH4dPyrEYwqDiRElI/p7sr6r8Yn2Rwk8hr+wsKiouroaCVjFA8OA2C9ozE2dr9GMR+K6IAIsspDLV3u7RlhY" +
  "4FQadqd/4xwGBBsb+6cnkqVPgyAxbGNwcLDHnKQpeCMGeGEGsmJFfn6z51r0n4CrY896z7lfVFQ0frxVZqaVVWXlEgEwFNDi" +
  "YfGD0Vy5/s+ANMWmdTVFg3ipjcUigODGxhlC1Gte/8ZgHtkadd85Y6K6BNfJipWYAD94comH+Jz4VxCcoFcXIYHQa1aZmZlW" +
  "lWdjLAFrSPfVTuHqj5TLU6Soz2oLm2VIoGaGABiwnVfTP3sjqoRDE8OwPwAY+8lRHfxipVKR3+zp4MnhdybflStX1PhF1cjA" +
  "KpTgW1VWnp04WoxVlDMyAlAgXSSXy/2H4WAUYVG1DCCsxbAGzzKm48zVMiPMpXsjY6Szg8d1lJWWK5WK5sWqheuPeLK5594J" +
  "j/iFhYWF44n/mZnXJs3PNLfq06eyUkv74pjBlt1aCgWC4auWtsrlvq2Yfa5eNq2LAKbgcOWxwGOeIU5Yhi3zloclOrpIJEJb" +
  "x9ke68YZtnTwS0v5BN9hisMRTxb/Bxb/kBq+ujp0PIYgM9PcKj7oWh9CQEtL+9HEgEhvS4lAIpEIh7uuCvdvrZKL5HJ567JP" +
  "F6XYyG1cAZjglmzDFsMWw2xDHPGyDVtaWtLnzZgxY156S0tLDS4GPl+pyF+scJCGqfbvJ/r/QAh0uV9UXRQaeq06kwQ/yH5k" +
  "JTLA6qvd99GjR3Fzw8PDw+fGVbVWVVVViUS+vq1YBmxscBnSYJvcgSOmYVnIuDIkgFbTguB4saysjF9erDAzUznYu+AaIAQ+" +
  "/4EV4EDheLLyi6qvTZp0zRwJmPexChw2Bhno6OAQ0LdvX7329nYRmr5ILhKJ4lxdUyxs0MKl7J0Yj5Bxe8YlzxEnYREuQ3xC" +
  "wrCmxtCwjC9TmJmZKRYvdIGFKk+2/B1d/wOuvgMHCseH4rpHAmNGmxMCmVZng0AapKPTBwsQS0GPmD6hIJo7DEC6KmJZeEQk" +
  "VxgoEE6Zgq0BFoRgwAkHw5qaGtRfZmZmZqZSOCQJIMmsmdTfgoLP37vC4heODy0kBDIzTbxZCfr06WPlDbzRI0kJNDbW7kZB" +
  "T69dL0BKGi9PIOCpNwZcI6ZpBmw9Qvh83F4Sw12OmZlCZeYwRwogDVFg799b4F6wcAIp/YWFReM/JPhFmVZWPqPVBM7GSADs" +
  "54/U0tExIgy+Njbui/a1sfMsjDoH2TWZUwzDzgQUiJNGRfHr6ppw5GxqamtrM1u7MEkMDAhHqbAOb91bUBA9YQlbequLxswc" +
  "X425Z2VVOVrs1IdYpY7WaJwKLWNjRhqTMnyStZEBrlwL7j4D9TAijmXS5OSQkKiokBAHh8lzknAlMzQPEwCrcEFBwQbJQo7A" +
  "tTHe4zPN1ZGPxdQjyTfRB8swSCzjY2OmOROLWT0Lv+mVNzUp/CAIhNIptrZCiYC0T4YBZrIKBwBCwH2C5HNS+6vHX5skXXEN" +
  "k++aVeUkiWQSYYCpN9IbGFZhnkQiFAolEkHXJPRv0GmGoSi6WwEnwWFAMlmlaCbj3173gvWU/eZDBw6cQwJBvKBr5ubmlZVW" +
  "lSaB4GPCEdDSnogMcPLrpi7bDF5+h5/mkpHCCJHNIgkTzYB0VBvue5v3b93rfvRze/A7fx5Hv2rza7HAEajUOhsPML+yjxG7" +
  "/k9O9GEf68Bgk9lW/d1dm2FNfAoELgum4EmdRkgWhOA0SPDd3NwnALPw/Plz585Vm5tb+YGflZrAfABJjI4RMtDW1j5pHCvU" +
  "fLKEAkGgq6tUY9vCGQ1ijxnj+AYzPLrt7GmGArGHGZlG8/MJ/kwAy81qAib24G1iXl+Plf/sGDGA5RgdIyMjHWNt7b4n2529" +
  "me7njhTFuEb4W9ikLLJ8we6ABmlyB06AdR3J5HgLUwEPsWeHNOEwoFDl7y1wR3wGRp8nBMxNMicJQTipDxLAwh8PDARO0zEy" +
  "QgmwAusFBPLYPQA2YwYicRC0sbFICXxOAwokyR18fl1dHZ/fkUxmN7wqTkpuQ3yVSnVkr7u7mztu+yQrfiQMzE0y5wMD8/vU" +
  "19eTfU8QHvbZT+M06IvFNy5glpTr7zR4y21sbHx9W+UWc3uenwAOSmEdfL7BqMmjSvn8Og9geAJbx7DgkKYmmRIN8d0KPp+A" +
  "Kelz/vz586fOZ5qYmPgAAz4m9fX1fbDzTQzEOQf7gJGRkTGWX32s/QGDXYcHooW3yn1XBwYukttYzHruLFKS3ME3SAKKSirl" +
  "l0eNGpW8fBwWRBnBV3nudXdzc19vieWFF/TjKTQTp/pJ+MSIcFK9NSGgdTEAt3YgmDnya5aBvj4yqKqqkvv6+vrG+cqrArAU" +
  "Lmq1iNDYJtNgO45fF0x2SaPqZOV1dViNWXiVwtPd3X2r24YJqDADPpsJfqaTSf17QAMDM+utSQy0tbW8cRtPQ+CHRiwFZIAc" +
  "5PIquY2Nr7xqMPB44Cq3SWG3KJ2G+yEkwDAMbKyTcaZUFiuaPdeudXff675hphi/msIM4AQwCbUnxwaWofXW1kZk/x9D9joM" +
  "COKDRuoYd8mg7+vrq6+nL69ajTGMtLFJkfYkQIEwpKkpCo+Xp0Q1yQyK29ra2lQqFXvmtfaH9RPEbJFmYCaLn+lkYj6f2+jM" +
  "r7e2tiad/+JocgkDFThT3Yq4ZjQyxrlKlDIcIDBFXhWheZBOweQ6WVNI0oKkkCZZ06gkj1EOalv/3gTSj9ih3nszEjhWqRaA" +
  "HAGGWhMJ+vY9iRVYXfQlgX7zY6ZNGzly5LRpMfNn2dM+cXKRb0REio28SjMJgXhuIGuSyWQGsihvoAVCodhWLBYKhaSLEHhg" +
  "wHLDj6eOHTt2ysTJyRwzgL06+qa1tTUWn76PnLnzKW5HIBCKLS2lYrEEowerRSJ5VZVc3oq5qCEBDXMMmgwMZDJZU1QSrqYu" +
  "cRiGhcfdy4qGY2gmTk71k9RrmQJB0E1r65uoQN/28M4TMvxg1/dji5PExuGaqArXyABiFMx2iJK1tZk5+JBCiD2ksy9yRwfS" +
  "FQ2mHL5JKNYA9avSaWoGeu1zh7F3nriBg8IiyJ1LUGA/OCJ8kau60PU0GiTecybPWdDtaTDcwC349NPhAnZ12a9oMEUC9U5O" +
  "TiZYE9WG9Q8Z9D158qSeKC4ec4AGnn3k6shAicYNSoZHv7QdQs/fuOZGJ/zjH/8Y+zGKwkxI6MJ38ushopqB9smTenrt+qsx" +
  "OuKAOFGVyDfctbu72JpePpZQDIMTSRe+YOFf/jIWzdQPXJaYkvhj/tWHTtCYrbACG920tjY+SebvubME4pgqX1+RSFTlG/+6" +
  "TzLQMPovY8cmrEj40XRswpIENvxWTk4m9R/iobvmmwWx1kY3rW8aI4N2UXi4yFd/boyzvqhq7otS7g/hSxPGjk3wFlgGmZqa" +
  "NhD3rUycTEwm+UlecGOepiAwyNrI6CbbhvTa9fXDLQWCWXGiqsGvS8BnrKkp9nihc4PpMa1jWlYmJiahH/phUXzB+7GPBL43" +
  "7SbXCfX1RbNwOA8QVQW83pMUNMSPHZtgjzVhScOAY2crQ0NDP5wZKHj5aM3QQIl9Yj/E6henp6/vCgwPBotE4a/3WBUNPqam" +
  "ppjtko8bTMfExtvbS7Eqvuj2cjcKADyx1N4yVk9fhNLzAkSigNdABzIkJpiaJswSWwY0mDYsYeOIQ+K//xSZAWmQTtTXj5sl" +
  "HRagr9/ec8n+JwxGN5iaNiTg8sebTRqPXbzU2BlwvkhfXxQX167f7vzc/PUHjQJJUEODKZJo+E+doEEaIyKTQHscKe2vy2Dm" +
  "GC1T02Nj/LrfPPiDDMSxc+P09eOcvf8XjxNRFIh9/PwmvI6GNDBSHz8/7+694DUY0OzvV6XeCxl076SvbzTZU7zuZ/9o3vZa" +
  "r/Var/Var/Var/Xa/2f7H6dKDQ/7dQTuAAAAAElFTkSuQmCC";
