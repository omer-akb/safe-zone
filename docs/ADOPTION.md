# How to Adopt Thyris Safe Zone

Adoption gives the Safe Zone community a clear, public reference showing where
TSZ is used. It is required for organizations covered by Section 10 of the
[license](../LICENSE) and optional for everyone else.

## Who must adopt?

Your organization must adopt TSZ when both conditions are true:

1. The applicable legal entity, including entities under common control, has
   500 or more employees.
2. It uses TSZ in, with, or to provide, operate, or secure a commercial product
   or service.

The license contains the controlling definitions and timing rules. If you are
unsure whether the requirement applies, obtain advice from your legal counsel.

## Adoption process

1. Fork the repository and edit [`ADOPTERS.md`](../ADOPTERS.md).
2. Add one table row in alphabetical order with:
   - the organization's legal or commonly recognized name and website;
   - a GitHub account for a person authorized to confirm the entry;
   - the environment, such as `Production` or `Internal production`;
   - a short, non-confidential description of how TSZ is used; and
   - a stable public reference controlled by the organization, such as a
     product page, engineering post, public repository, or official
     announcement, that identifies the organization as a TSZ adopter.
3. Open a pull request titled `Add <organization> as a TSZ adopter`.
4. Include the following confirmation in the pull request description:

   > I confirm that I am authorized to submit this adopter entry on behalf of
   > the organization and that the information is accurate and may be
   > published.

5. Respond to any reasonable maintainer request needed to verify or publish
   the entry. The entry is adopted when the pull request is merged. A complete
   request submitted on time remains compliant while it is under review, as
   described in Section 10.2 of the license.

Do not include confidential architecture, personal data beyond the authorized
contact, customer information, security-sensitive details, or metrics that the
organization has not approved for publication.

## If a pull request is not possible

Email `open-source@thyris.ai` with the five entry fields and the authorization
confirmation above. Use the subject `TSZ adoption — <organization>`. The
maintainers can open the repository change on the organization's behalf.

## Updating or removing an entry

Open a pull request with the corrected information or email
`open-source@thyris.ai`. An organization may request removal when it no longer
uses TSZ in a way covered by Section 10. The maintainers may correct or remove
entries that are inaccurate, unverifiable, misleading, or no longer current.

## No endorsement

An entry records adoption only. It does not grant trademark rights or imply
that Thyris.AI endorses the adopter, or that the adopter endorses Thyris.AI.
