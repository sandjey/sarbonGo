--
-- PostgreSQL database dump
--

\restrict M0nz6uk0g11WT9v3jNJIVb4Shkhrgc6dA4ckySdc8WbWYzLiajHuygnzU0aVJSL

-- Dumped from database version 18.3
-- Dumped by pg_dump version 18.3

SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET transaction_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;

SET default_tablespace = '';

SET default_table_access_method = heap;

--
-- Name: route_points; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.route_points (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    cargo_id uuid NOT NULL,
    type character varying NOT NULL,
    address character varying NOT NULL,
    lat double precision NOT NULL,
    lng double precision NOT NULL,
    comment character varying,
    point_order integer NOT NULL,
    is_main_load boolean DEFAULT false NOT NULL,
    is_main_unload boolean DEFAULT false NOT NULL,
    city_code character varying(20),
    region_code character varying(20),
    orientir character varying(500),
    place_id character varying(255),
    point_at timestamp with time zone,
    country_code character varying(3),
    CONSTRAINT route_points_type_check CHECK (((type)::text = ANY ((ARRAY['LOAD'::character varying, 'UNLOAD'::character varying, 'CUSTOMS'::character varying, 'TRANSIT'::character varying])::text[])))
);


--
-- Name: route_points route_points_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.route_points
    ADD CONSTRAINT route_points_pkey PRIMARY KEY (id);


--
-- Name: idx_route_points_cargo_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_route_points_cargo_id ON public.route_points USING btree (cargo_id);


--
-- Name: idx_route_points_country_code; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_route_points_country_code ON public.route_points USING btree (country_code);


--
-- Name: route_points route_points_cargo_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.route_points
    ADD CONSTRAINT route_points_cargo_id_fkey FOREIGN KEY (cargo_id) REFERENCES public.cargo(id) ON DELETE CASCADE;


--
-- PostgreSQL database dump complete
--

\unrestrict M0nz6uk0g11WT9v3jNJIVb4Shkhrgc6dA4ckySdc8WbWYzLiajHuygnzU0aVJSL

