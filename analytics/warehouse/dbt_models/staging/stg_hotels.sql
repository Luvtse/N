-- ============================================================================
-- Staging Model: Hotels
-- Source: raw_haven.hotels
-- Purpose: Clean and standardize hotel data
-- ============================================================================

{{
    config(
        materialized='view',
        tags=['staging', 'haven', 'hotels']
    )
}}

with source as (
    select * from {{ source('raw_haven', 'hotels') }}
),

cleaned as (
    select
        -- Primary key
        id as hotel_id,
        
        -- Basic info
        name as hotel_name,
        description,
        
        -- Location
        country_code,
        city,
        address,
        latitude,
        longitude,
        
        -- Rating & Pricing
        star_rating,
        price_per_night,
        currency,
        
        -- Amenities & Images (JSON arrays)
        amenities,
        images,
        
        -- Status
        status,
        
        -- Audit
        created_at,
        updated_at,
        
        -- Data quality flags
        case when latitude is null or longitude is null then true else false end as missing_coordinates,
        case when price_per_night is null or price_per_night = 0 then true else false end as missing_price,
        case when star_rating is null then true else false end as missing_rating
        
    from source
    where id is not null
),

final as (
    select
        *,
        -- Star rating categorization
        case 
            when star_rating = 5 then 'Luxury'
            when star_rating = 4 then 'Upper Upscale'
            when star_rating = 3 then 'Upscale'
            when star_rating = 2 then 'Midscale'
            when star_rating = 1 then 'Economy'
            else 'Unrated'
        end as hotel_category,
        
        -- Price tier
        case 
            when price_per_night >= 500 then 'Luxury'
            when price_per_night >= 250 then 'Premium'
            when price_per_night >= 100 then 'Standard'
            when price_per_night >= 50 then 'Budget'
            else 'Economy'
        end as price_tier,
        
        -- Geographic region
        case 
            when country_code in ('US', 'CA', 'MX') then 'North America'
            when country_code in ('GB', 'FR', 'DE', 'ES', 'IT') then 'Europe'
            when country_code in ('CN', 'JP', 'KR', 'SG') then 'Asia Pacific'
            when country_code in ('BR', 'AR', 'CL') then 'Latin America'
            else 'Other'
        end as geographic_region,
        
        -- Amenity flags
        case when amenities::text like '%WiFi%' then true else false end as has_wifi,
        case when amenities::text like '%Pool%' then true else false end as has_pool,
        case when amenities::text like '%Gym%' then true else false end as has_gym,
        case when amenities::text like '%Spa%' then true else false end as has_spa,
        case when amenities::text like '%Restaurant%' then true else false end as has_restaurant,
        case when amenities::text like '%Bar%' then true else false end as has_bar,
        case when amenities::text like '%Parking%' then true else false end as has_parking,
        case when amenities::text like '%Airport%' then true else false end as has_airport_shuttle
        
    from cleaned
    where status = 'active'
)

select * from final